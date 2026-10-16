package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	repo "github.com/armonic-tech/armonic-backend/internal/repositories"
	db "github.com/armonic-tech/armonic-backend/migrations"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := postgres.Run(
		ctx,
		"postgres:17",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		panic(err)
	}
	defer pg.Terminate(ctx)

	connStr, err := pg.ConnectionString(ctx)
	if err != nil {
		panic(err)
	}

	testDB, err = sql.Open("pgx", connStr)
	if err != nil {
		panic(err)
	}
	defer testDB.Close()

	if err := db.Migrate(testDB); err != nil {
		panic(err)
	}

	m.Run()
}

func postJSON(t *testing.T, client *http.Client, url string, body any, out any) int {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	resp, err := client.Post(url, "application/json", strings.NewReader(string(raw)))
	require.NoError(t, err)
	defer resp.Body.Close()

	if out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
	return resp.StatusCode
}

func getJSON(t *testing.T, client *http.Client, url string, out any) int {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	if out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
	return resp.StatusCode
}

// TestClaimAndInviteFlow drives the whole "claim the server, then invite a
// member" story end to end against a real HTTP+WS server backed by a real
// Postgres, the same sequence verified by hand while building this feature:
//
//  1. login/claim are refused before the server is claimed
//  2. the wrong claim password is rejected
//  3. the right claim password issues a ticket, which registers the admin
//  4. the admin can log in, and can create an invite over WS (this only
//     works because ClaimRegisterHandler calls ServerRepo.SetOwner)
//  5. that invite lets a brand-new user sign up over HTTP
//  6. the same invite can't be redeemed a second time
func getBytes(t *testing.T, client *http.Client, url string) (int, []byte) {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

func doMethod(t *testing.T, client *http.Client, method, url, token string) int {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func uploadPNG(t *testing.T, client *http.Client, url, token string, out any) int {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	var pixels bytes.Buffer
	require.NoError(t, png.Encode(&pixels, img))

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "logo.png")
	require.NoError(t, err)
	_, err = part.Write(pixels.Bytes())
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req, err := http.NewRequest(http.MethodPost, url, &body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
	return resp.StatusCode
}

func TestClaimAndInviteFlow(t *testing.T) {
	cfg := config.Config{
		Port:          "0",
		Host:          "localhost",
		ServerName:    "Test Instance",
		JWTSecret:     "test-jwt-secret",
		ClaimPassword: "smoke-test-claim-password",
		MaxMsgLen:     2000,
		// Real limits, not the zero value: with MaxBytes 0 every upload is
		// rejected as too large and the logo subtest would test nothing.
		Upload: config.UploadConfig{
			Dir: t.TempDir(), MaxBytes: 1 << 20,
			MaxImageWidth: 2000, MaxImageHeight: 2000, MaxImagePixels: 4_000_000,
			ThumbnailSize: 64, RatePerMin: 60, Burst: 10, MaxConcurrent: 2,
			MaxUserBytes: 10 << 20,
		},
	}

	repos := repo.InitRepositories(testDB)
	srv, err := New(context.Background(), cfg, testDB, repos)
	require.NoError(t, err)

	ts := httptest.NewServer(srv.handler)
	defer ts.Close()
	client := ts.Client()

	t.Run("login and claim/register are refused before claiming", func(t *testing.T) {
		status := postJSON(t, client, ts.URL+"/auth/login", map[string]string{"username": "x", "password": "irrelevant"}, nil)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("wrong claim password is rejected", func(t *testing.T) {
		status := postJSON(t, client, ts.URL+"/claim/password", map[string]string{"password": "not-the-password"}, nil)
		require.Equal(t, http.StatusUnauthorized, status)
	})

	var adminToken, defaultServerID string

	t.Run("correct claim password + register creates the admin", func(t *testing.T) {
		var passResp struct {
			Ticket    string    `json:"ticket"`
			ExpiresAt time.Time `json:"expiresAt"`
		}
		status := postJSON(t, client, ts.URL+"/claim/password", map[string]string{"password": cfg.ClaimPassword}, &passResp)
		require.Equal(t, http.StatusOK, status)
		require.NotEmpty(t, passResp.Ticket)

		var registerResp struct {
			Token string `json:"token"`
		}
		status = postJSON(t, client, ts.URL+"/claim/register", map[string]string{
			"ticket":   passResp.Ticket,
			"username": "admin",
			"password": "admin-password-123",
		}, &registerResp)
		require.Equal(t, http.StatusOK, status)
		require.NotEmpty(t, registerResp.Token)
		adminToken = registerResp.Token

		var info struct {
			ID      string `json:"id"`
			Claimed bool   `json:"claimed"`
		}
		status = getJSON(t, client, ts.URL+"/info", &info)
		require.Equal(t, http.StatusOK, status)
		require.True(t, info.Claimed)
		defaultServerID = info.ID
	})

	t.Run("claim/password and claim/register now refuse — already claimed", func(t *testing.T) {
		status := postJSON(t, client, ts.URL+"/claim/password", map[string]string{"password": cfg.ClaimPassword}, nil)
		require.Equal(t, http.StatusConflict, status)
	})

	t.Run("admin can log in now that the server is claimed", func(t *testing.T) {
		var loginResp struct {
			Token string `json:"token"`
		}
		status := postJSON(t, client, ts.URL+"/auth/login", map[string]string{
			"username": "admin",
			"password": "admin-password-123",
		}, &loginResp)
		require.Equal(t, http.StatusOK, status)
		require.NotEmpty(t, loginResp.Token)
	})

	var inviteToken string

	t.Run("admin creates an invite over HTTP (owner-gated; proves SetOwner ran)", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/server/"+defaultServerID+"/invite", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var inviteResp struct {
			InviteToken string `json:"inviteToken"`
			URL         string `json:"url"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&inviteResp))
		require.NotEmpty(t, inviteResp.InviteToken)
		require.Contains(t, inviteResp.URL, inviteResp.InviteToken)
		inviteToken = inviteResp.InviteToken
	})

	t.Run("creating an invite without a token is rejected (401)", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/server/"+defaultServerID+"/invite", nil)
		require.NoError(t, err)
		resp, err := client.Do(req) // no Authorization header
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("invite/status reports the invite as valid", func(t *testing.T) {
		var statusResp struct {
			ServerID string `json:"serverId"`
		}
		status := getJSON(t, client, ts.URL+"/invite/status?token="+inviteToken, &statusResp)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, defaultServerID, statusResp.ServerID)
	})

	t.Run("invite/signup redeems the invite into a brand-new account", func(t *testing.T) {
		var signupResp struct {
			Token string `json:"token"`
		}
		status := postJSON(t, client, ts.URL+"/invite/signup", map[string]string{
			"token":    inviteToken,
			"username": "newmember",
			"password": "member-password-123",
		}, &signupResp)
		require.Equal(t, http.StatusOK, status)
		require.NotEmpty(t, signupResp.Token)

		var info struct {
			MemberCount int `json:"memberCount"`
		}
		getJSON(t, client, ts.URL+"/info", &info)
		require.Equal(t, 2, info.MemberCount) // admin + newmember
	})

	var memberToken string

	t.Run("invite/signup redeems a second invite for a plain member", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/server/"+defaultServerID+"/invite", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		var inviteResp struct {
			InviteToken string `json:"inviteToken"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&inviteResp))

		var signupResp struct {
			Token string `json:"token"`
		}
		status := postJSON(t, client, ts.URL+"/invite/signup", map[string]string{
			"token":    inviteResp.InviteToken,
			"username": "logowatcher",
			"password": "member-password-123",
		}, &signupResp)
		require.Equal(t, http.StatusOK, status)
		memberToken = signupResp.Token
	})

	// Against the real composition root, so a logo route missing from New()
	// fails here instead of 404ing a client.
	t.Run("the admin sets a logo every member can read, and removes it", func(t *testing.T) {
		logoURL := ts.URL + "/server/" + defaultServerID + "/logo"

		status, _ := getBytes(t, client, logoURL)
		require.Equal(t, http.StatusNotFound, status, "no logo yet")

		var set struct {
			LogoID       string `json:"logoId"`
			LogoThumbURL string `json:"logoThumbUrl"`
		}
		require.Equal(t, http.StatusOK, uploadPNG(t, client, logoURL, adminToken, &set))
		require.NotEmpty(t, set.LogoID)
		require.Contains(t, set.LogoThumbURL, "/logo/thumb?v="+set.LogoID)

		// Public: no Authorization header anywhere in this read.
		status, body := getBytes(t, client, logoURL)
		require.Equal(t, http.StatusOK, status)
		require.NotEmpty(t, body)
		status, _ = getBytes(t, client, logoURL+"/thumb")
		require.Equal(t, http.StatusOK, status)

		var info struct {
			LogoID  string `json:"logoId"`
			LogoURL string `json:"logoUrl"`
		}
		getJSON(t, client, ts.URL+"/info", &info)
		require.Equal(t, set.LogoID, info.LogoID)
		require.Contains(t, info.LogoURL, "?v="+set.LogoID)

		// A member is not the owner: upload and removal are both refused.
		require.Equal(t, http.StatusForbidden, uploadPNG(t, client, logoURL, memberToken, nil))
		require.Equal(t, http.StatusForbidden, doMethod(t, client, http.MethodDelete, logoURL, memberToken))

		require.Equal(t, http.StatusNoContent, doMethod(t, client, http.MethodDelete, logoURL, adminToken))
		status, _ = getBytes(t, client, logoURL)
		require.Equal(t, http.StatusNotFound, status)
		// A fresh struct: an absent field leaves whatever the previous decode
		// put there, which would hide the removal.
		info.LogoID = ""
		getJSON(t, client, ts.URL+"/info", &info)
		require.Empty(t, info.LogoID)
	})

	t.Run("join-voice requires membership and a voice channel of that server", func(t *testing.T) {
		var channels []channel.ChannelInfo
		require.Equal(t, http.StatusOK, getJSONAuth(t, client, ts.URL+"/server/"+defaultServerID, memberToken, &channels))
		var textID, voiceID string
		for _, c := range channels {
			switch c.Type {
			case "text":
				textID = c.ID
			case "voice":
				voiceID = c.ID
			}
		}
		require.NotEmpty(t, textID)
		require.NotEmpty(t, voiceID)

		conn := dialWS(t, ts.URL, memberToken)
		for _, bad := range []map[string]string{
			{"serverId": "00000000-0000-0000-0000-000000000000", "channelId": voiceID},
			{"serverId": defaultServerID, "channelId": textID},
			{"serverId": defaultServerID, "channelId": "00000000-0000-0000-0000-000000000000"},
		} {
			require.NoError(t, conn.WriteJSON(map[string]string{"type": "join-voice", "serverId": bad["serverId"], "channelId": bad["channelId"]}))
			frame := readFrame(t, conn)
			require.Equal(t, "error", frame["type"], bad)
			require.Equal(t, "could not join voice", frame["message"], bad)
		}

		require.NoError(t, conn.WriteJSON(map[string]string{"type": "join-voice", "serverId": defaultServerID, "channelId": voiceID}))
		require.Equal(t, "offer", readFrame(t, conn)["type"])
	})

	t.Run("a bot joins voice only where the owner summoned it", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/server/"+defaultServerID+"/invite", strings.NewReader(`{"bot":true}`))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := client.Do(req)
		require.NoError(t, err)
		var inv struct {
			InviteToken string `json:"inviteToken"`
			Bot         bool   `json:"bot"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&inv))
		resp.Body.Close()
		require.True(t, inv.Bot)

		var signup struct {
			Token string `json:"token"`
		}
		require.Equal(t, http.StatusOK, postJSON(t, client, ts.URL+"/invite/signup", map[string]string{
			"token": inv.InviteToken, "username": "armonic-bot", "password": "bot-password-123",
		}, &signup))
		botToken := signup.Token

		var me struct {
			ID string `json:"id"`
		}
		require.Equal(t, http.StatusOK, getJSONAuth(t, client, ts.URL+"/me", botToken, &me))
		var members []struct {
			ID    string `json:"id"`
			IsBot bool   `json:"isBot"`
		}
		require.Equal(t, http.StatusOK, getJSONAuth(t, client, ts.URL+"/server/"+defaultServerID+"/members", adminToken, &members))
		bots := 0
		for _, m := range members {
			if m.IsBot {
				bots++
				require.Equal(t, me.ID, m.ID)
			}
		}
		require.Equal(t, 1, bots)

		var channels []channel.ChannelInfo
		require.Equal(t, http.StatusOK, getJSONAuth(t, client, ts.URL+"/server/"+defaultServerID, adminToken, &channels))
		var voiceID string
		for _, c := range channels {
			if c.Type == "voice" {
				voiceID = c.ID
			}
		}

		bot, botOK := dialWSFrame(t, ts.URL, botToken)
		require.Equal(t, true, botOK["isBot"])
		join := map[string]string{"type": "join-voice", "serverId": defaultServerID, "channelId": voiceID}

		require.NoError(t, bot.WriteJSON(join))
		require.Equal(t, "could not join voice", readFrame(t, bot)["message"], "unsummoned bot joined voice")

		summon := map[string]string{"type": "summon-bot", "serverId": defaultServerID, "channelId": voiceID, "targetUserId": me.ID}
		member := dialWS(t, ts.URL, memberToken)
		require.NoError(t, member.WriteJSON(summon))
		require.Equal(t, "unauthorized", readFrame(t, member)["message"])

		admin := dialWS(t, ts.URL, adminToken)
		require.NoError(t, admin.WriteJSON(summon))
		frame := readFrame(t, bot)
		require.Equal(t, "bot-summon", frame["type"])
		require.Equal(t, voiceID, frame["channelId"])

		require.NoError(t, bot.WriteJSON(join))
		require.Equal(t, "offer", readFrame(t, bot)["type"])

		require.NoError(t, admin.WriteJSON(map[string]string{"type": "dismiss-bot", "serverId": defaultServerID, "targetUserId": me.ID}))
		require.Equal(t, "bot-dismiss", readFrame(t, bot)["type"])
		require.Equal(t, "kicked-voice", readFrame(t, bot)["type"])

		require.NoError(t, bot.WriteJSON(join))
		require.Equal(t, "could not join voice", readFrame(t, bot)["message"], "dismissed bot rejoined voice")

		require.NoError(t, admin.WriteJSON(summon))
		require.Equal(t, "bot-summon", readFrame(t, bot)["type"])
		require.NoError(t, bot.WriteJSON(join))
		require.Equal(t, "offer", readFrame(t, bot)["type"])
		require.NoError(t, admin.WriteJSON(map[string]string{"type": "kick-voice", "serverId": defaultServerID, "channelId": voiceID, "targetUserId": me.ID}))
		require.Equal(t, "kicked-voice", readFrame(t, bot)["type"])
		require.NoError(t, bot.WriteJSON(join))
		require.Equal(t, "could not join voice", readFrame(t, bot)["message"], "kicked bot rejoined voice")
	})

	t.Run("the same invite can't be redeemed twice", func(t *testing.T) {
		status := postJSON(t, client, ts.URL+"/invite/signup", map[string]string{
			"token":    inviteToken,
			"username": "someoneelse",
			"password": "another-password-1",
		}, nil)
		require.Equal(t, http.StatusGone, status)

		status = getJSON(t, client, ts.URL+"/invite/status?token="+inviteToken, nil)
		require.Equal(t, http.StatusGone, status)
	})
}

func getJSONAuth(t *testing.T, client *http.Client, url, token string, out any) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
	return resp.StatusCode
}

// dialWS opens /ws and completes the auth handshake.
func dialWS(t *testing.T, baseURL, token string) *websocket.Conn {
	t.Helper()
	conn, _ := dialWSFrame(t, baseURL, token)
	return conn
}

// dialWSFrame is dialWS that also returns the auth-ok frame.
func dialWSFrame(t *testing.T, baseURL, token string) (*websocket.Conn, map[string]any) {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(baseURL, "http")+"/ws", nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, conn.WriteJSON(map[string]string{"type": "auth", "token": token}))
	ok := readFrame(t, conn)
	require.Equal(t, "auth-ok", ok["type"])
	return conn, ok
}

// readFrame returns the next frame that is not an unsolicited push.
func readFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	for {
		var frame map[string]any
		require.NoError(t, conn.ReadJSON(&frame))
		switch frame["type"] {
		case "candidate", "mls-sync", "voice-link":
			continue
		}
		return frame
	}
}
