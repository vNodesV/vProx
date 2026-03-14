// Package configwizard provides an interactive configuration wizard for vProx TOML files.
// Both terminal (vprox config) and web (vprox config --web) modes share the same
// validation logic and write-with-backup semantics.
package configwizard

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// validCountries is the set of supported probe country codes (mirrors countryNodes in vOps).
var validCountries = map[string]bool{
	"CA": true, "US": true, "FR": true, "DE": true, "NL": true,
	"GB": true, "UK": true, "FI": true, "JP": true, "SG": true,
	"BR": true, "IN": true,
}

// reSlug matches a valid chain name slug: lowercase alphanumeric and hyphens.
var reSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*$`)

var stdin = bufio.NewReader(os.Stdin)

// readLine reads a raw line from stdin, trimming whitespace.
func readLine() string {
	s, _ := stdin.ReadString('\n')
	return strings.TrimSpace(s)
}

// prompt prints "  label [default]: " and returns the entered value.
// If the user presses Enter without input, the default is returned.
func prompt(label, def string) string {
	if def != "" {
		fmt.Printf("  %s [%s]: ", label, def)
	} else {
		fmt.Printf("  %s: ", label)
	}
	in := readLine()
	if in == "" {
		return def
	}
	return in
}

// readString prompts for a free-form string. Never returns empty if required=true.
func readString(label, def string, required bool) string {
	for {
		v := prompt(label, def)
		if v != "" {
			return v
		}
		if !required {
			return ""
		}
		fmt.Printf("  ✗ %s is required\n", label)
	}
}

// readBool prompts for a boolean. Accepts y/yes/true/1 and n/no/false/0.
func readBool(label string, def bool) bool {
	defStr := "false"
	if def {
		defStr = "true"
	}
	for {
		v := prompt(label+" (true/false)", defStr)
		switch strings.ToLower(v) {
		case "y", "yes", "true", "1":
			return true
		case "n", "no", "false", "0":
			return false
		default:
			fmt.Printf("  ✗ enter true or false\n")
		}
	}
}

// readInt prompts for an integer in [min, max]. Pass max=0 to skip upper bound check.
func readInt(label string, def, min, max int) int {
	for {
		raw := prompt(label, strconv.Itoa(def))
		n, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Printf("  ✗ enter a whole number\n")
			continue
		}
		if n < min {
			fmt.Printf("  ✗ minimum is %d\n", min)
			continue
		}
		if max > 0 && n > max {
			fmt.Printf("  ✗ maximum is %d\n", max)
			continue
		}
		return n
	}
}

// readFloat prompts for a float > 0.
func readFloat(label string, def float64) float64 {
	for {
		raw := prompt(label, strconv.FormatFloat(def, 'f', -1, 64))
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || f <= 0 {
			fmt.Printf("  ✗ enter a positive decimal number\n")
			continue
		}
		return f
	}
}

// readPort prompts for a TCP port (1–65535).
func readPort(label string, def int) int {
	return readInt(label, def, 1, 65535)
}

// readOptionalPort prompts for a TCP port or 0 (disabled). Returns 0 on empty input when def=0.
func readOptionalPort(label string, def int) int {
	for {
		d := ""
		if def > 0 {
			d = strconv.Itoa(def)
		}
		if d != "" {
			fmt.Printf("  %s [%s]: ", label, d)
		} else {
			fmt.Printf("  %s (0 or empty = disabled): ", label)
		}
		raw := readLine()
		if raw == "" {
			return def
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Printf("  ✗ enter a port number (1–65535) or 0 to disable\n")
			continue
		}
		if n == 0 {
			return 0
		}
		if n < 1 || n > 65535 {
			fmt.Printf("  ✗ port must be between 1 and 65535\n")
			continue
		}
		return n
	}
}

// readIP prompts for a valid IP address.
func readIP(label, def string, required bool) string {
	for {
		v := prompt(label, def)
		if v == "" && !required {
			return ""
		}
		if net.ParseIP(strings.TrimSpace(v)) != nil {
			return strings.TrimSpace(v)
		}
		fmt.Printf("  ✗ invalid IP address: %q\n", v)
	}
}

// readOptionalIP prompts for an IP or empty.
func readOptionalIP(label, def string) string {
	return readIP(label, def, false)
}

// readCIDRList prompts for a comma-separated list of CIDRs.
// Returns the existing list on empty input.
func readCIDRList(label string, def []string) []string {
	defStr := strings.Join(def, ",")
	for {
		raw := prompt(label+" (comma-separated CIDRs)", defStr)
		if raw == "" {
			return def
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		ok := true
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, _, err := net.ParseCIDR(p); err != nil {
				fmt.Printf("  ✗ invalid CIDR: %q\n", p)
				ok = false
				break
			}
			out = append(out, p)
		}
		if ok {
			return out
		}
	}
}

// readSlug prompts for a chain name slug: lowercase alphanumeric+hyphens.
func readSlug(label, def string) string {
	for {
		v := strings.ToLower(strings.TrimSpace(prompt(label, def)))
		if v == "" {
			fmt.Printf("  ✗ %s is required\n", label)
			continue
		}
		if !reSlug.MatchString(v) {
			fmt.Printf("  ✗ use lowercase letters, digits and hyphens only\n")
			continue
		}
		return v
	}
}

// readCountry prompts for a supported probe country code.
func readCountry(label, def string) string {
	codes := make([]string, 0, len(validCountries))
	for k := range validCountries {
		codes = append(codes, k)
	}
	hint := strings.Join(codes, "|")
	for {
		v := strings.ToUpper(strings.TrimSpace(prompt(label+" ("+hint+")", def)))
		if v == "" && def != "" {
			return def
		}
		if validCountries[v] {
			return v
		}
		fmt.Printf("  ✗ unsupported country code: %q\n", v)
	}
}

// readPassword prompts for a plain-text password, hashes it with bcrypt Cost=12,
// and returns the hash. Returns "" if the user leaves it empty.
func readPassword(label string) string {
	fmt.Printf("  %s (leave empty to disable auth): ", label)
	raw := readLine()
	if raw == "" {
		return ""
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			fmt.Printf("  ✗ password contains invalid characters\n")
			return readPassword(label)
		}
	}
	h, err := bcrypt.GenerateFromPassword([]byte(raw), 12)
	if err != nil {
		fmt.Printf("  ✗ failed to hash password: %v\n", err)
		return readPassword(label)
	}
	return string(h)
}

// readStringList prompts for a comma-separated list of strings.
func readStringList(label string, def []string) []string {
	defStr := strings.Join(def, ",")
	raw := prompt(label+" (comma-separated)", defStr)
	if raw == "" {
		return def
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// readOptionalURL prompts for an optional URL string (no validation beyond non-empty).
func readOptionalURL(label, def string) string {
	return prompt(label+" (optional URL)", def)
}

// confirm asks a yes/no question; returns true on y/yes.
func confirm(question string, def bool) bool {
	return readBool(question, def)
}

// section prints a decorated section header.
func section(title string) {
	fmt.Printf("\n── %s %s\n", title, strings.Repeat("─", max(0, 60-len(title)-4)))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
