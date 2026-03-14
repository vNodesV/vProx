// Package ssh provides a lightweight SSH client for the fleet module.
// It opens one session per command; callers are responsible for closing
// the Client when done.
package ssh

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Client wraps an active SSH connection.
type Client struct {
	c *ssh.Client
}

// expandPath resolves ~ and $HOME in path strings from TOML values.
func expandPath(p string) string {
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			p = h + p[1:]
		}
	}
	return p
}

// Dial opens an SSH connection to host:port authenticating with the private
// key at keyPath.  When knownHostsPath is non-empty the host key is verified
// against that file; otherwise the connection proceeds without verification
// and a warning is logged.
func Dial(host string, port int, user, keyPath, knownHostsPath string) (*Client, error) {
	keyPath = expandPath(keyPath)

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("fleet/ssh: read key %s: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("fleet/ssh: parse key: %w", err)
	}

	var hostKeyCallback ssh.HostKeyCallback
	if knownHostsPath != "" {
		knownHostsPath = expandPath(knownHostsPath)
		cb, khErr := knownhosts.New(knownHostsPath)
		if khErr != nil {
			return nil, fmt.Errorf("fleet/ssh: load known_hosts %s: %w", knownHostsPath, khErr)
		}
		hostKeyCallback = cb
	} else {
		log.Printf("[fleet/ssh] WARNING: known_hosts_path not configured — host key verification disabled for %s", host)
		hostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec // known_hosts_path not configured; warn logged above
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	c, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("fleet/ssh: dial %s: %w", addr, err)
	}

	return &Client{c: c}, nil
}

// Run executes cmd on the remote host and returns combined stdout+stderr.
// A non-zero exit code is returned as an error alongside any output.
func (c *Client) Run(cmd string) (string, error) {
	sess, err := c.c.NewSession()
	if err != nil {
		return "", fmt.Errorf("fleet/ssh: new session: %w", err)
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(cmd)
	return string(out), err
}

// Close releases the underlying SSH connection.
func (c *Client) Close() error { return c.c.Close() }
