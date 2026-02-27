package ufw

import (
	"fmt"
	"net"
	"os/exec"
)

// IsAvailable reports whether ufw is installed and accessible.
func IsAvailable() bool {
	_, err := exec.LookPath("ufw")
	return err == nil
}

// Block adds a UFW deny rule for ip. Returns nil if ufw is not installed (soft fail).
// ip is validated with net.ParseIP before any exec call.
func Block(ip string) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("ufw: invalid IP address: %q", ip)
	}
	if !IsAvailable() {
		return nil // ufw not installed — soft fail, DB block still applies
	}
	cmd := exec.Command("sudo", "/usr/sbin/ufw", "deny", "from", ip)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ufw deny %s: %w: %s", ip, err, string(out))
	}
	return nil
}

// Unblock removes the UFW deny rule for ip. Returns nil if ufw is not installed.
func Unblock(ip string) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("ufw: invalid IP address: %q", ip)
	}
	if !IsAvailable() {
		return nil
	}
	cmd := exec.Command("sudo", "/usr/sbin/ufw", "delete", "deny", "from", ip)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ufw delete deny %s: %w: %s", ip, err, string(out))
	}
	return nil
}
