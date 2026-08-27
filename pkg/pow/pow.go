package pow

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	Algorithm = "SHA-256"

	saltBytes = 16

	domainSeparator = "armonic/pow/v1"
)

var (
	ErrDisabled  = errors.New("proof of work is disabled")
	ErrMalformed = errors.New("malformed proof of work")
	ErrExpired   = errors.New("proof of work expired")
	ErrInvalid   = errors.New("invalid proof of work")
	ErrReplay    = errors.New("proof of work already used")
)

type Challenge struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	MaxNumber int64  `json:"maxnumber"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

type solution struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	Number    int64  `json:"number"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

type Manager struct {
	enabled   bool
	key       []byte
	maxNumber int64
	ttl       time.Duration
	seen      *replayStore
	now       func() time.Time
}

func New(enabled bool, secret string, maxNumber int64, ttl time.Duration) *Manager {
	if maxNumber < 1 {
		maxNumber = 1
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(domainSeparator))

	return &Manager{
		enabled:   enabled,
		key:       mac.Sum(nil),
		maxNumber: maxNumber,
		ttl:       ttl,
		seen:      newReplayStore(),
		now:       time.Now,
	}
}

func (m *Manager) Enabled() bool { return m.enabled }

func (m *Manager) Issue() (Challenge, error) {
	if !m.enabled {
		return Challenge{}, ErrDisabled
	}

	raw := make([]byte, saltBytes)
	if _, err := rand.Read(raw); err != nil {
		return Challenge{}, fmt.Errorf("pow: generating salt: %w", err)
	}
	salt := fmt.Sprintf("%s?expires=%d", hex.EncodeToString(raw), m.now().Add(m.ttl).Unix())

	secret, err := rand.Int(rand.Reader, bigMax(m.maxNumber))
	if err != nil {
		return Challenge{}, fmt.Errorf("pow: generating secret number: %w", err)
	}
	challenge := hashHex(salt, secret.Int64())

	return Challenge{
		Algorithm: Algorithm,
		Challenge: challenge,
		MaxNumber: m.maxNumber,
		Salt:      salt,
		Signature: m.sign(challenge),
	}, nil
}

func (m *Manager) Verify(payload string) error {
	if !m.enabled {
		return nil
	}
	if payload == "" {
		return ErrMalformed
	}

	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ErrMalformed
	}
	var s solution
	if err := json.Unmarshal(raw, &s); err != nil {
		return ErrMalformed
	}
	if s.Algorithm != Algorithm || s.Challenge == "" || s.Salt == "" || s.Number < 0 {
		return ErrMalformed
	}

	expires, ok := saltExpiry(s.Salt)
	if !ok {
		return ErrMalformed
	}
	if m.now().After(expires) {
		return ErrExpired
	}
	if !hmac.Equal([]byte(m.sign(s.Challenge)), []byte(s.Signature)) {
		return ErrInvalid
	}
	if !hmac.Equal([]byte(hashHex(s.Salt, s.Number)), []byte(s.Challenge)) {
		return ErrInvalid
	}
	if !m.seen.claim(s.Challenge, expires, m.now()) {
		return ErrReplay
	}
	return nil
}

func (m *Manager) sign(challenge string) string {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(challenge))
	return hex.EncodeToString(mac.Sum(nil))
}

func hashHex(salt string, number int64) string {
	sum := sha256.Sum256([]byte(salt + strconv.FormatInt(number, 10)))
	return hex.EncodeToString(sum[:])
}

func saltExpiry(salt string) (time.Time, bool) {
	_, query, found := strings.Cut(salt, "?")
	if !found {
		return time.Time{}, false
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		return time.Time{}, false
	}
	unix, err := strconv.ParseInt(params.Get("expires"), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(unix, 0), true
}
