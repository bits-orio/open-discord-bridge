package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	content := "# a comment\n" +
		"\n" +
		"DISCORD_BOT_TOKEN=from-file\n" +
		"export FACTORIO_RCON_PASSWORD=\"qu=oted#with-specials\"\n" +
		"ODB_PREEXISTING=from-file\n" +
		"malformed-line-no-equals\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Unset (not t.Setenv(..., "")) the two we expect from the file: loadDotEnv skips keys
	// that are already SET — even to empty — so they must be truly absent for the file value
	// to apply. The third is pre-set below to prove an existing env var is NOT overridden.
	os.Unsetenv("DISCORD_BOT_TOKEN")
	os.Unsetenv("FACTORIO_RCON_PASSWORD")
	t.Cleanup(func() {
		os.Unsetenv("DISCORD_BOT_TOKEN")
		os.Unsetenv("FACTORIO_RCON_PASSWORD")
	})
	t.Setenv("ODB_PREEXISTING", "from-env")

	loadDotEnv(dir)

	if got := os.Getenv("DISCORD_BOT_TOKEN"); got != "from-file" {
		t.Errorf("DISCORD_BOT_TOKEN = %q, want %q", got, "from-file")
	}
	// Quotes stripped; '=' and '#' inside the value preserved.
	if got := os.Getenv("FACTORIO_RCON_PASSWORD"); got != "qu=oted#with-specials" {
		t.Errorf("FACTORIO_RCON_PASSWORD = %q, want %q", got, "qu=oted#with-specials")
	}
	// Pre-existing environment must win over the file.
	if got := os.Getenv("ODB_PREEXISTING"); got != "from-env" {
		t.Errorf("ODB_PREEXISTING = %q, want %q (env must win)", got, "from-env")
	}
}

func TestLoadDotEnvMissing(t *testing.T) {
	loadDotEnv(t.TempDir()) // no .env present — must be a silent no-op, not a panic/error
}

// TestLoadDotEnvSingleQuotedEscaping mirrors the format the setup wizard's shQuote writes
// (see wizard/cmd/wizard/envfile.go), so this loader and bash agree on the value when a
// secret contains a single quote, '$', ';', or a backtick.
func TestLoadDotEnvSingleQuotedEscaping(t *testing.T) {
	dir := t.TempDir()
	secret := `it's a $ecret; with ` + "`backticks`"
	// Same encoding as shQuote: wrap in '...', escaping embedded ' as '\''.
	encoded := "'" + strings.ReplaceAll(secret, "'", `'\''`) + "'"
	content := "FACTORIO_RCON_PASSWORD=" + encoded + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("FACTORIO_RCON_PASSWORD")
	t.Cleanup(func() { os.Unsetenv("FACTORIO_RCON_PASSWORD") })

	loadDotEnv(dir)

	if got := os.Getenv("FACTORIO_RCON_PASSWORD"); got != secret {
		t.Errorf("FACTORIO_RCON_PASSWORD = %q, want %q", got, secret)
	}
}

func TestLoadDotEnvWarnsOnPermissiveMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode bits don't apply on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ODB_UNUSED_PERM_TEST=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// os.WriteFile's mode is subject to umask; force the exact mode we're testing.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("ODB_UNUSED_PERM_TEST")
	t.Cleanup(func() { os.Unsetenv("ODB_UNUSED_PERM_TEST") })

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	loadDotEnv(dir)

	if got := buf.String(); !bytes.Contains([]byte(got), []byte("chmod 600")) {
		t.Errorf("expected a chmod 600 warning for a group/world-readable .env, got log output: %q", got)
	}
}

func TestLoadDotEnvNoWarningOnStrictMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode bits don't apply on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ODB_UNUSED_PERM_TEST2=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("ODB_UNUSED_PERM_TEST2")
	t.Cleanup(func() { os.Unsetenv("ODB_UNUSED_PERM_TEST2") })

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	loadDotEnv(dir)

	if got := buf.String(); bytes.Contains([]byte(got), []byte("chmod 600")) {
		t.Errorf("did not expect a permission warning for a 0600 .env, got log output: %q", got)
	}
}
