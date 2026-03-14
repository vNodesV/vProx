package ufw

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

// IsAvailable reports whether ufw is installed at the canonical path.
// Uses os.Stat on the absolute path because Block/Unblock invoke /usr/sbin/ufw
// directly, and systemd PATH may not include /usr/sbin.
func IsAvailable() bool {
	_, err := os.Stat("/usr/sbin/ufw")
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

// ListBlocked runs "sudo -n ufw status numbered" and returns all IPs with a DENY rule.
// Returns (nil, nil) when ufw is not installed. CIDR subnets are skipped; only
// host addresses are returned so they can be matched against ip_accounts.
// Requires a sudoers NOPASSWD entry for the vops process user:
//
//	Cmnd_Alias VLOG_UFW = /usr/sbin/ufw deny from *, /usr/sbin/ufw delete deny from *, /usr/sbin/ufw status numbered
//	www-data ALL=(ALL) NOPASSWD: VLOG_UFW
func ListBlocked() ([]string, error) {
	if !IsAvailable() {
		return nil, nil
	}
	cmd := exec.Command("sudo", "-n", "/usr/sbin/ufw", "status", "numbered")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ufw status: %w: %s", err, string(out))
	}
	return parseUFWDenyIPs(string(out)), nil
}

// parseUFWDenyIPs extracts host IPs from "ufw status numbered" output.
// Lines look like: "[ 3] Anywhere DENY IN  203.0.113.5"
func parseUFWDenyIPs(output string) []string {
	var ips []string
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "DENY") {
			continue
		}
		// Extract last whitespace-delimited token that looks like an IP (no slash → host addr)
		fields := strings.Fields(line)
		for i := len(fields) - 1; i >= 0; i-- {
			f := fields[i]
			if strings.Contains(f, "/") {
				break // CIDR — skip
			}
			if ip := net.ParseIP(f); ip != nil {
				if !seen[f] {
					seen[f] = true
					ips = append(ips, f)
				}
				break
			}
		}
	}
	return ips
}
