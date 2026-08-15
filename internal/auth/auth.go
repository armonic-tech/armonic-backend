package auth

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUsernameTaken      = errors.New("username taken")
	ErrInvalidInput       = errors.New("username required and password must be at least 8 characters")
)

const tokenTTL = 7 * 24 * time.Hour

// Claims is the JWT payload. Sub is the user ID (users.id) — the same
// identity used throughout the domain layer (user.User.ID, membership rows).
type Claims struct {
	Sub string `json:"sub"`
	jwt.RegisteredClaims
}

// Credentials is the subset of UserRepo this package needs to persist and
// look up login credentials, kept separate from repositories.UserRepo so this
// package doesn't depend on the repositories package.
type Credentials interface {
	Create(ctx context.Context, id, username, passwordHash string) error
	GetByUsername(ctx context.Context, username string) (id, passwordHash string, err error)
}

// Service implements signup/login and issues/validates the JWTs consumed by
// handlers.Authenticator.
type Service struct {
	secret []byte
	users  Credentials
}

func NewService(secret string, users Credentials) *Service {
	return &Service{secret: []byte(secret), users: users}
}

func (s *Service) Signup(ctx context.Context, username, password string) (string, error) {
	if username == "" || len(password) < 8 {
		return "", ErrInvalidInput
	}
	if _, _, err := s.users.GetByUsername(ctx, username); err == nil {
		return "", ErrUsernameTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	id := uuid.New().String()
	if err := s.users.Create(ctx, id, username, string(hash)); err != nil {
		return "", err
	}
	return s.issue(id)
}

func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	id, hash, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return "", ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", ErrInvalidCredentials
	}
	return s.issue(id)
}

func (s *Service) Validate(ctx context.Context, tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidCredentials
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidCredentials
	}
	return claims, nil
}

func (s *Service) issue(userID string) (string, error) {
	claims := Claims{
		Sub: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}
