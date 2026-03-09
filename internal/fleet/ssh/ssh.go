// Package ssh provides a lightweight SSH client for the fleet module.
// It opens one session per command; callers are responsible for closing
// the Client when done.
package ssh

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Client wraps an active SSH connection.
type Client struct {
	c *ssh.Client
}

// Dial opens an SSH connection to host:port authenticating with the
// ed25519 private key at keyPath.
//
// NOTE: HostKeyCallback is intentionally permissive for now.
// TODO: validate against a known_hosts file before production use.
func Dial(host string, port int, user, keyPath string) (*Client, error) {
	// Expand $HOME / ~ so TOML values like "$HOME/.ssh/id_key" work.
	keyPath = os.ExpandEnv(keyPath)
	if strings.HasPrefix(keyPath, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			keyPath = h + keyPath[1:]
		}
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("fleet/ssh: read key %s: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("fleet/ssh: parse key: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // known_hosts TODO
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
