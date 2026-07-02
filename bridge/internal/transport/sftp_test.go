package transport

import (
	"os"
	"path/filepath"
	"testing"
)

// hostKeyCallback only touches config fields; it never dials, so it's safe to unit test
// directly (no live SFTP server needed).

func TestHostKeyCallbackRefusesInsecureWithPasswordByDefault(t *testing.T) {
	r := &sftpReader{cfg: SFTPConfig{Password: "secret"}}
	if _, err := r.hostKeyCallback(); err == nil {
		t.Fatal("expected an error: password auth with no known_hosts_path and no opt-in must refuse to connect insecurely")
	}
}

func TestHostKeyCallbackAllowsInsecureWithPasswordWhenOptedIn(t *testing.T) {
	r := &sftpReader{cfg: SFTPConfig{Password: "secret", AllowInsecureHostKey: true}}
	cb, err := r.hostKeyCallback()
	if err != nil {
		t.Fatalf("expected the opt-in flag to allow an insecure connection, got: %v", err)
	}
	if cb == nil {
		t.Fatal("expected a non-nil host key callback")
	}
}

func TestHostKeyCallbackAllowsInsecureForKeyOnlyAuth(t *testing.T) {
	// Password auth is the specific MITM risk being guarded against (a stolen
	// credential); key-only auth with no known_hosts_path keeps today's (pre-existing,
	// logged-warning) behavior rather than newly breaking existing key-only setups.
	r := &sftpReader{cfg: SFTPConfig{KeyPath: "/some/key"}}
	cb, err := r.hostKeyCallback()
	if err != nil {
		t.Fatalf("key-only auth without known_hosts_path should not hard-fail, got: %v", err)
	}
	if cb == nil {
		t.Fatal("expected a non-nil host key callback")
	}
}

func TestHostKeyCallbackUsesKnownHosts(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	r := &sftpReader{cfg: SFTPConfig{Password: "secret", KnownHostsPath: khPath}}
	cb, err := r.hostKeyCallback()
	if err != nil {
		t.Fatalf("valid known_hosts_path should be accepted, got: %v", err)
	}
	if cb == nil {
		t.Fatal("expected a non-nil host key callback")
	}
}
