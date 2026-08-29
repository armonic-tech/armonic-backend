package pow

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret"

func newTestManager(t *testing.T, maxNumber int64) *Manager {
	t.Helper()
	return New(true, testSecret, maxNumber, 5*time.Minute)
}

func solve(t *testing.T, c Challenge) string {
	t.Helper()
	for n := int64(0); n <= c.MaxNumber; n++ {
		sum := sha256.Sum256([]byte(c.Salt + strconv.FormatInt(n, 10)))
		if hex.EncodeToString(sum[:]) == c.Challenge {
			return encodeSolution(t, solution{
				Algorithm: c.Algorithm, Challenge: c.Challenge,
				Number: n, Salt: c.Salt, Signature: c.Signature,
			})
		}
	}
	t.Fatal("challenge had no solution in range")
	return ""
}

func encodeSolution(t *testing.T, s solution) string {
	t.Helper()
	raw, err := json.Marshal(s)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

func decodeSolution(t *testing.T, payload string) solution {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err)
	var s solution
	require.NoError(t, json.Unmarshal(raw, &s))
	return s
}

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	m := newTestManager(t, 500)

	c, err := m.Issue()
	require.NoError(t, err)
	require.Equal(t, Algorithm, c.Algorithm)
	require.Equal(t, int64(500), c.MaxNumber)
	require.Contains(t, c.Salt, "?expires=")
	require.Len(t, c.Challenge, sha256.Size*2)

	require.NoError(t, m.Verify(solve(t, c)))
}

func TestVerifyRejectsReplay(t *testing.T) {
	// The whole point of a work factor is that it must be paid again; a
	// solution that can be resubmitted costs nothing after the first time.
	m := newTestManager(t, 500)
	c, err := m.Issue()
	require.NoError(t, err)

	payload := solve(t, c)
	require.NoError(t, m.Verify(payload))
	require.ErrorIs(t, m.Verify(payload), ErrReplay)
}

func TestVerifyRejectsWrongNumber(t *testing.T) {
	m := newTestManager(t, 500)
	c, err := m.Issue()
	require.NoError(t, err)

	s := decodeSolution(t, solve(t, c))
	s.Number++
	require.ErrorIs(t, m.Verify(encodeSolution(t, s)), ErrInvalid)
}

func TestVerifyRejectsForgedChallenge(t *testing.T) {
	// A client that invents its own easy challenge cannot sign it.
	m := newTestManager(t, 500)

	salt := "deadbeef?expires=" + strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)
	sum := sha256.Sum256([]byte(salt + "0"))
	forged := solution{
		Algorithm: Algorithm,
		Challenge: hex.EncodeToString(sum[:]),
		Number:    0,
		Salt:      salt,
		Signature: strings.Repeat("00", sha256.Size),
	}
	require.ErrorIs(t, m.Verify(encodeSolution(t, forged)), ErrInvalid)
}

func TestVerifyRejectsSignatureFromAnotherInstance(t *testing.T) {
	issuer := New(true, "instance-a-secret", 500, time.Minute)
	verifier := New(true, "instance-b-secret", 500, time.Minute)

	c, err := issuer.Issue()
	require.NoError(t, err)
	require.ErrorIs(t, verifier.Verify(solve(t, c)), ErrInvalid)
}

func TestVerifyRejectsExpired(t *testing.T) {
	m := newTestManager(t, 500)
	now := time.Now()
	m.now = func() time.Time { return now }

	c, err := m.Issue()
	require.NoError(t, err)
	payload := solve(t, c)

	now = now.Add(6 * time.Minute)
	require.ErrorIs(t, m.Verify(payload), ErrExpired)
}

func TestVerifyRejectsMalformedPayloads(t *testing.T) {
	m := newTestManager(t, 500)

	cases := map[string]string{
		"empty":       "",
		"not base64":  "!!!not base64!!!",
		"not json":    base64.StdEncoding.EncodeToString([]byte("hello")),
		"wrong algo":  encodeSolution(t, solution{Algorithm: "MD5", Challenge: "x", Salt: "y?expires=1"}),
		"no salt":     encodeSolution(t, solution{Algorithm: Algorithm, Challenge: "x"}),
		"salt no exp": encodeSolution(t, solution{Algorithm: Algorithm, Challenge: "x", Salt: "nosuffix"}),
		"negative n":  encodeSolution(t, solution{Algorithm: Algorithm, Challenge: "x", Salt: "y?expires=1", Number: -1}),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, m.Verify(payload), ErrMalformed)
		})
	}
}

func TestDisabledManagerAcceptsAnythingAndIssuesNothing(t *testing.T) {
	m := New(false, testSecret, 500, time.Minute)

	require.False(t, m.Enabled())
	require.NoError(t, m.Verify(""))
	require.NoError(t, m.Verify("garbage"))

	_, err := m.Issue()
	require.ErrorIs(t, err, ErrDisabled)
}

func TestIssueProducesDistinctChallenges(t *testing.T) {
	m := newTestManager(t, 500)

	seen := make(map[string]bool)
	for range 20 {
		c, err := m.Issue()
		require.NoError(t, err)
		require.False(t, seen[c.Challenge], "challenge repeated")
		seen[c.Challenge] = true
	}
}

func TestReplayStoreEvictsExpiredEntries(t *testing.T) {
	r := newReplayStore()
	now := time.Now()

	require.True(t, r.claim("a", now.Add(time.Minute), now))
	require.False(t, r.claim("a", now.Add(time.Minute), now))
	require.Len(t, r.seen, 1)

	later := now.Add(2 * time.Minute)
	require.True(t, r.claim("b", later.Add(time.Minute), later))
	require.Len(t, r.seen, 1)
	require.NotContains(t, r.seen, "a")
}
