package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestShQuote(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"plain", "simple-value"},
		{"single-quote", "it's-tricky"},
		{"dollar-and-semicolon", "a$b;c"},
		{"backtick", "a`whoami`b"},
		{"spaces", "has spaces here"},
		{"combo", `it's a $ecret;`},
		{"only-quotes", "'''"},
		{"empty", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shQuote(c.in)
			if len(got) < 2 || got[0] != '\'' || got[len(got)-1] != '\'' {
				t.Fatalf("shQuote(%q) = %q, want a single-quoted string", c.in, got)
			}
		})
	}
}

// TestShQuoteRoundTripsThroughShell sources a generated .env-style line with `sh -c` and
// checks the shell recovers the exact original value — the actual failure mode this fix
// addresses (start-*.sh do `set -a; . bridge/.env`).
func TestShQuoteRoundTripsThroughShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	secret := `it's a $ecret; with ` + "`backticks`" + ` and spaces`

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := "FACTORIO_RCON_PASSWORD=" + shQuote(secret) + "\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("sh", "-c", ". "+envPath+" && printf '%s' \"$FACTORIO_RCON_PASSWORD\"").CombinedOutput()
	if err != nil {
		t.Fatalf("sourcing generated .env failed: %v\noutput: %s", err, out)
	}
	if got := string(out); got != secret {
		t.Errorf("round-tripped value = %q, want %q", got, secret)
	}
}
