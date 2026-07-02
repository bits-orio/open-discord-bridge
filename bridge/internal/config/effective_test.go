package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setEnvMode sets the minimal env vars for a valid env-var-mode config.
func setEnvMode(t *testing.T) {
	t.Helper()
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "s3cretpw!")
	t.Setenv("DISCORD_BOT_TOKEN", "t0ken-value-xyz")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "12345")
}

func readEffective(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(EffectiveConfigName)
	if err != nil {
		t.Fatalf("read %s: %v", EffectiveConfigName, err)
	}
	return string(b)
}

func TestEffectiveDumpWrittenWithoutSecrets(t *testing.T) {
	t.Chdir(t.TempDir())
	setEnvMode(t)

	if _, err := Load("/no/such/bridge.yaml"); err != nil {
		t.Fatalf("env load: %v", err)
	}
	out := readEffective(t)

	for _, secret := range []string{"s3cretpw!", "t0ken-value-xyz"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret value %q leaked into effective config:\n%s", secret, out)
		}
	}
	for _, want := range []string{
		"env-var mode",
		"Validation:   OK",
		"FACTORIO_RCON_PASSWORD: SET (9 chars)",
		"DISCORD_BOT_TOKEN: SET (15 chars)",
		"poll_interval: 1s", // Duration must marshal as a string, not nanoseconds
		"address: game:27015",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("effective config missing %q:\n%s", want, out)
		}
	}
}

func TestEffectiveDumpWrittenOnValidationFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	setEnvMode(t)
	t.Setenv("FACTORIO_RCON_PASSWORD", "") // the AleForge case

	if _, err := Load("/no/such/bridge.yaml"); err == nil {
		t.Fatal("expected validation error for empty RCON password")
	}
	out := readEffective(t)

	if !strings.Contains(out, "Validation:   FAILED") || !strings.Contains(out, "RCON password is empty") {
		t.Fatalf("effective config should record the validation failure:\n%s", out)
	}
	if !strings.Contains(out, "FACTORIO_RCON_PASSWORD: MISSING") {
		t.Fatalf("effective config should mark the missing secret:\n%s", out)
	}
}

func TestUnknownKeysWarnButLoad(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("FACTORIO_RCON_PASSWORD", "s3cretpw!")
	t.Setenv("DISCORD_BOT_TOKEN", "t0ken-value-xyz")

	// A top-level rcon: block (AleForge's injected keys) is not part of the schema.
	cfgFile := filepath.Join(t.TempDir(), "bridge.yaml")
	yaml := `
factorio:
  rcon:
    address: 127.0.0.1:27015
    password_env: FACTORIO_RCON_PASSWORD
  events_file: /tmp/events.jsonl
rcon:
  address: 127.0.0.1:33641
  password: testing123
discord:
  token_env: DISCORD_BOT_TOKEN
  routes:
    - source: "*"
      channel_id: "111"
`
	if err := os.WriteFile(cfgFile, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("unknown keys must warn, not fail: %v", err)
	}
	if c.Factorio.RCON.Address != "127.0.0.1:27015" {
		t.Fatalf("config not loaded: %+v", c.Factorio)
	}
	out := readEffective(t)
	if !strings.Contains(out, `unknown key "rcon"`) {
		t.Fatalf("effective config should warn about the unknown top-level rcon key:\n%s", out)
	}
	// The injected literal password must not leak into the dump either.
	if strings.Contains(out, "testing123") {
		t.Fatalf("ignored unknown-key value leaked into effective config:\n%s", out)
	}
	if !strings.Contains(out, "file mode") {
		t.Fatalf("effective config should state file mode:\n%s", out)
	}
}

func TestForcedEnvModeIgnoresConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	setEnvMode(t)
	t.Setenv("ODB_CONFIG", "none")

	// A config file exists at the default path, but ODB_CONFIG=none must ignore it.
	cfgFile := filepath.Join(dir, "bridge.yaml")
	if err := os.WriteFile(cfgFile, []byte("factorio:\n  events_file: /from/file.jsonl\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("forced env mode: %v", err)
	}
	if c.Factorio.EventsFile != "/tmp/events.jsonl" {
		t.Fatalf("config came from the file, not env: %+v", c.Factorio)
	}
	out := readEffective(t)
	if !strings.Contains(out, "ODB_CONFIG=none") {
		t.Fatalf("effective config should state env mode was forced:\n%s", out)
	}
}
