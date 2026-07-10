package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/armonic-tech/armonic-backend/config"
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
func TestClaimAndInviteFlow(t *testing.T) {
	cfg := config.Config{
		Port:          "0",
		Host:          "localhost",
		ServerName:    "Test Instance",
		JWTSecret:     "test-jwt-secret",
		ClaimPassword: "smoke-test-claim-password",
		MaxMsgLen:     2000,
	}

	repos := repo.InitRepositories(testDB)
	srv, err := New(context.Background(), cfg, repos)
	require.NoError(t, err)

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()
	client := ts.Client()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

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

	t.Run("admin creates an invite over WS (proves SetOwner ran)", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		defer conn.Close()

		require.NoError(t, conn.WriteJSON(map[string]any{"type": "auth", "token": adminToken, "name": "Admin"}))
		var authResp struct {
			Type string `json:"type"`
		}
		require.NoError(t, conn.ReadJSON(&authResp))
		require.Equal(t, "auth-ok", authResp.Type)

		require.NoError(t, conn.WriteJSON(map[string]any{"type": "create-invite", "serverId": defaultServerID}))
		var inviteResp struct {
			Type        string `json:"type"`
			InviteToken string `json:"inviteToken"`
		}
		require.NoError(t, conn.ReadJSON(&inviteResp))
		require.Equal(t, "invite-created", inviteResp.Type)
		require.NotEmpty(t, inviteResp.InviteToken)
		inviteToken = inviteResp.InviteToken
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
