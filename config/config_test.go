package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClaimPasswordEnvNeedsNoConfigFile(t *testing.T) {
	t.Setenv("CLAIM_PASSWORD", "from-env")
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing.json"))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClaimPassword != "from-env" {
		t.Fatalf("claim password %q, want the env value", cfg.ClaimPassword)
	}
}

func TestClaimPasswordEnvWinsOverTheFile(t *testing.T) {
	t.Setenv("CLAIM_PASSWORD", "from-env")
	t.Setenv("CONFIG_FILE", writeConfigFile(t, `{"app":{"password":"from-file"}}`))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClaimPassword != "from-env" {
		t.Fatalf("claim password %q, want the env value", cfg.ClaimPassword)
	}
}

func TestClaimPasswordFallsBackToTheFile(t *testing.T) {
	t.Setenv("CLAIM_PASSWORD", "")
	t.Setenv("CONFIG_FILE", writeConfigFile(t, `{"app":{"password":"from-file"}}`))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClaimPassword != "from-file" {
		t.Fatalf("claim password %q, want the file value", cfg.ClaimPassword)
	}
}

func TestNoClaimPasswordAnywhereRefusesToStart(t *testing.T) {
	t.Setenv("CLAIM_PASSWORD", "")
	t.Setenv("CONFIG_FILE", writeConfigFile(t, `{"app":{}}`))
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "CLAIM_PASSWORD") {
		t.Fatalf("want an error naming CLAIM_PASSWORD, got %v", err)
	}
}
