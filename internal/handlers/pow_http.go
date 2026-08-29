package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/armonic-tech/armonic-backend/pkg/pow"
)

type PowIssuer interface {
	Enabled() bool
	Issue() (pow.Challenge, error)
}

type PowChecker interface {
	Verify(payload string) error
}

// @Summary      Proof-of-work challenge
// @Description  Issues an Altcha-compatible challenge. Solve it and send the base64 solution in the "altcha" field of the login, claim or invite-signup request. Returns 404 when POW_ENABLED is off.
// @Tags         Users
// @Produce      json
// @Success      200 {object} pow.Challenge
// @Failure      404 {string} string "proof of work is disabled"
// @Router       /pow/challenge [get]
func PowChallenge(issuer PowIssuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !issuer.Enabled() {
			http.Error(w, "proof of work is disabled", http.StatusNotFound)
			return
		}

		challenge, err := issuer.Issue()
		if err != nil {
			slog.ErrorContext(r.Context(), "pow: error issuing challenge", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(challenge)
	}
}

func checkPow(w http.ResponseWriter, verifier PowChecker, payload string) bool {
	err := verifier.Verify(payload)
	if err == nil {
		return true
	}
	status := http.StatusBadRequest
	if errors.Is(err, pow.ErrExpired) || errors.Is(err, pow.ErrReplay) {
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
	return false
}
