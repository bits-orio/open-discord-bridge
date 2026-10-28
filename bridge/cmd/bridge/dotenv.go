package main

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv loads KEY=VALUE pairs from a .env file in dir into the process environment,
// but only for keys that are not already set — so a real exported environment (e.g. a
// hosting panel's injected variables) always wins. The bridge reads its secrets from the
// environment, and the setup wizard writes a .env next to bridge.yaml; loading it here lets
// `odb-bridge -config bridge.yaml` work without the caller having to `source .env` first.
// A missing or unreadable .env is not an error.
func loadDotEnv(dir string) {
	path := filepath.Join(dir, ".env")
	f, err := os.Open(path)
	if err != nil {
		return // no .env — fine, the env may be populated directly
	}
	defer f.Close()

	warnIfDotEnvPermissive(path, f)

	loaded := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue // already in the environment — do not override
		}
		if err := os.Setenv(key, unquote(strings.TrimSpace(val))); err == nil {
			loaded++
		}
	}
	if loaded > 0 {
		log.Printf("config: loaded %d secret(s)/var(s) from %s", loaded, path)
	}
}

// warnIfDotEnvPermissive logs a loud, hard-to-miss warning if .env is readable or writable
// by anyone other than its owner. It typically holds live secrets (bot token, RCON/SFTP
// passwords); group/world access almost always means a permissive umask rather than
// intentional sharing. This only warns — it doesn't refuse to start — so it stays
// backward-compatible with existing setups while still surfacing the risk.
func warnIfDotEnvPermissive(path string, f *os.File) {
	info, err := f.Stat()
	if err != nil {
		return // can't stat — nothing useful to warn about
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		log.Printf("config: WARNING %s is readable/writable by group or other (mode %04o) and "+
			"may contain secrets (bot token, RCON/SFTP passwords) — run: chmod 600 %s", path, mode, path)
	}
}

// unquote strips a single pair of matching surrounding quotes, if present. Double-quoted
// (and legacy plain single-quoted) values are taken literally; single-quoted values are
// additionally shell-unescaped (see unescapeSingleQuoted) since the setup wizard writes
// secrets that way so start-*.sh can safely `source` the same file — this keeps both
// readers of .env (this loader, and bash) agreeing on the value. No inline-comment
// stripping either way — a '#' may be part of a secret.
func unquote(s string) string {
	if len(s) >= 2 {
		switch c := s[0]; {
		case c == '\'' && s[len(s)-1] == '\'':
			return unescapeSingleQuoted(s)
		case c == '"' && s[len(s)-1] == c:
			return s[1 : len(s)-1]
		}
	}
	return s
}

// unescapeSingleQuoted reverses the POSIX single-quote encoding the wizard's shQuote
// produces: everything between a pair of single quotes is literal, and the four-character
// close-quote/backslash/quote/quote sequence represents one embedded literal single quote
// (close the quote, emit a backslash-escaped literal quote, reopen the quote) — the same
// grammar bash uses outside of single-quoted text, so a value round-trips identically
// whether it's read here or sourced by a shell.
func unescapeSingleQuoted(s string) string {
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote:
			if c == '\'' {
				inQuote = false
			} else {
				b.WriteByte(c)
			}
		case c == '\'':
			inQuote = true
		case c == '\\' && i+1 < len(s):
			b.WriteByte(s[i+1])
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
