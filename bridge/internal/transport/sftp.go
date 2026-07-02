package transport

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SFTPConfig describes how to reach the remote events file over SFTP. Use this when the
// bridge runs on separate infra from Factorio (e.g. Pterodactyl's per-server SFTP).
type SFTPConfig struct {
	Host           string // host:port (":22" appended if no port)
	User           string
	KeyPath        string // private key file for key auth
	Password       string // password auth (alternative/additional to KeyPath)
	KnownHostsPath string // known_hosts file; required when Password is set (see AllowInsecureHostKey)

	// AllowInsecureHostKey opts into connecting without host key verification when
	// KnownHostsPath is empty and Password auth is used. Without this, a network
	// attacker could MITM the connection and capture the SFTP password on any
	// reconnect. Off by default: ensure() refuses to connect in that combination
	// rather than silently degrading to an insecure connection.
	AllowInsecureHostKey bool
}

// sftpReader reads the events file over SFTP, lazily (re)connecting. A connection-level
// Stat/Read error (network failure, auth failure, broken session) drops the connection so
// the next poll reconnects. A "file not found" error is NOT connection-level — the SSH
// session is left intact and only the next Stat/Read is retried, so a momentarily-absent
// events file (server not started yet, mod hasn't written its first event) doesn't cause a
// full SSH re-dial every poll tick (which risks fail2ban/rate-limit lockout on the remote).
type sftpReader struct {
	cfg  SFTPConfig
	path string

	mu   sync.Mutex
	ssh  *ssh.Client
	sftp *sftp.Client
}

var insecureHostKeyWarn sync.Once

func newSFTPReader(cfg SFTPConfig, path string) *sftpReader {
	if cfg.Host != "" && !strings.Contains(cfg.Host, ":") {
		cfg.Host += ":22"
	}
	return &sftpReader{cfg: cfg, path: path}
}

func (s *sftpReader) ensure() error {
	if s.sftp != nil {
		return nil
	}
	auth, err := s.authMethods()
	if err != nil {
		return err
	}
	hostKey, err := s.hostKeyCallback()
	if err != nil {
		return err
	}
	client, err := ssh.Dial("tcp", s.cfg.Host, &ssh.ClientConfig{
		User:            s.cfg.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         10 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", s.cfg.Host, err)
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return fmt.Errorf("sftp client: %w", err)
	}
	s.ssh, s.sftp = client, sc
	return nil
}

func (s *sftpReader) reset() {
	if s.sftp != nil {
		s.sftp.Close()
		s.sftp = nil
	}
	if s.ssh != nil {
		s.ssh.Close()
		s.ssh = nil
	}
}

func (s *sftpReader) Stat() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return 0, err
	}
	fi, err := s.sftp.Stat(s.path)
	if err != nil {
		s.resetUnlessNotExist(err)
		return 0, err
	}
	return fi.Size(), nil
}

func (s *sftpReader) Read(offset int64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		s.reset()
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		s.reset()
		return nil, err
	}
	return data, nil
}

// Head returns up to n bytes from the start of the file (fewer if the file is shorter),
// used to fingerprint the file's identity for truncation detection.
func (s *sftpReader) Head(n int64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	m, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		s.reset()
		return nil, err
	}
	return buf[:m], nil
}

// open ensures a connection and opens the remote file, called with s.mu held. A
// not-found error is returned without dropping the connection (see resetUnlessNotExist).
func (s *sftpReader) open() (*sftp.File, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	f, err := s.sftp.Open(s.path)
	if err != nil {
		s.resetUnlessNotExist(err)
		return nil, err
	}
	return f, nil
}

// resetUnlessNotExist drops the SSH session unless err is a "file does not exist" error —
// that's an expected, transient state (the events file hasn't been created yet) that
// doesn't indicate anything is wrong with the connection, so re-dialing on it would just
// hammer the remote host with a fresh SSH handshake every poll tick.
func (s *sftpReader) resetUnlessNotExist(err error) {
	if !errors.Is(err, fs.ErrNotExist) {
		s.reset()
	}
}

func (s *sftpReader) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
	return nil
}

func (s *sftpReader) authMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if s.cfg.KeyPath != "" {
		pem, err := os.ReadFile(s.cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("read key %s: %w", s.cfg.KeyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil {
			return nil, fmt.Errorf("parse key %s: %w", s.cfg.KeyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if s.cfg.Password != "" {
		methods = append(methods, ssh.Password(s.cfg.Password))
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("sftp: no auth configured (set key_path or a password)")
	}
	return methods, nil
}

func (s *sftpReader) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if s.cfg.KnownHostsPath != "" {
		cb, err := knownhosts.New(s.cfg.KnownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("known_hosts %s: %w", s.cfg.KnownHostsPath, err)
		}
		return cb, nil
	}
	// No known_hosts configured. With password auth this is a real credential-theft
	// risk (a MITM on any reconnect can capture the password), so refuse to connect
	// unless the operator has explicitly opted into the risk. This mirrors the
	// (fail-fast, startup-time) check in config.validate(); it's duplicated here as a
	// defense-in-depth backstop for any caller that constructs SFTPConfig directly
	// without going through config loading.
	if s.cfg.Password != "" && !s.cfg.AllowInsecureHostKey {
		return nil, fmt.Errorf("sftp: known_hosts_path is not set and password auth is in use; refusing to connect without host key verification (a MITM could capture the password on reconnect). Set sftp.known_hosts_path, or set sftp.allow_insecure_host_key: true to explicitly accept the risk")
	}
	insecureHostKeyWarn.Do(func() {
		log.Printf("transport(sftp): WARNING host key verification disabled (set sftp.known_hosts_path to enable)")
	})
	return ssh.InsecureIgnoreHostKey(), nil
}

// NewSFTP tails a remote file over SFTP by polling.
func NewSFTP(cfg SFTPConfig, path string, interval time.Duration) *Tailer {
	return &Tailer{reader: newSFTPReader(cfg, path), interval: interval}
}
