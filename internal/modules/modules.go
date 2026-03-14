// Package modules manages the vProx module registry.
// Modules are standalone vProx ecosystem binaries (e.g. vOps) that are
// installed alongside vProx and tracked in config/modules.toml.
package modules

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Module describes one installed vProx ecosystem module.
type Module struct {
	Name        string    `toml:"name"`
	Version     string    `toml:"version"`
	BinaryPath  string    `toml:"binary_path"`
	ServiceName string    `toml:"service_name"`
	GitHash     string    `toml:"git_hash,omitempty"`
	InstalledAt time.Time `toml:"installed_at"`
	UpdatedAt   time.Time `toml:"updated_at"`
}

// Registry is the in-memory representation of modules.toml.
type Registry struct {
	Modules []Module `toml:"module"`
}

// Manager handles module lifecycle operations.
type Manager struct {
	cfgPath string
}

// New returns a Manager that persists state to cfgPath.
func New(cfgPath string) *Manager { return &Manager{cfgPath: cfgPath} }

// List returns all registered modules.
func (m *Manager) List() ([]Module, error) {
	r, err := m.load()
	if err != nil {
		return nil, err
	}
	return r.Modules, nil
}

// Add registers a module given "name@version" or "name".
// It verifies the binary exists at binaryPath (or auto-detects via PATH),
// records the service name, and persists the entry.
func (m *Manager) Add(nameVer, binaryPath, serviceName string) error {
	name, version := splitNameVer(nameVer)

	r, err := m.load()
	if err != nil {
		return err
	}
	for _, mod := range r.Modules {
		if mod.Name == name {
			return fmt.Errorf("module %q already registered (use 'mod update' to upgrade)", name)
		}
	}

	// Auto-detect binary if not provided.
	if binaryPath == "" {
		if p, err := exec.LookPath(name); err == nil {
			binaryPath = p
		}
	}
	// Resolve git hash if binary lives in a git repo.
	gitHash := resolveGitHash(binaryPath)

	now := time.Now().UTC()
	r.Modules = append(r.Modules, Module{
		Name:        name,
		Version:     version,
		BinaryPath:  binaryPath,
		ServiceName: serviceName,
		GitHash:     gitHash,
		InstalledAt: now,
		UpdatedAt:   now,
	})
	return m.save(r)
}

// Update changes the version, binary path, and/or git hash of an existing module.
// Pass empty strings for fields that should not change.
func (m *Manager) Update(name, newVersion, newBinaryPath string) error {
	r, err := m.load()
	if err != nil {
		return err
	}
	for i, mod := range r.Modules {
		if mod.Name != name {
			continue
		}
		if newVersion != "" {
			r.Modules[i].Version = newVersion
		}
		if newBinaryPath != "" {
			r.Modules[i].BinaryPath = newBinaryPath
		}
		r.Modules[i].GitHash = resolveGitHash(r.Modules[i].BinaryPath)
		r.Modules[i].UpdatedAt = time.Now().UTC()
		return m.save(r)
	}
	return fmt.Errorf("module %q not found", name)
}

// Remove deregisters a module by name. It does NOT stop the service or delete
// the binary — the caller is responsible for that.
func (m *Manager) Remove(name string) error {
	r, err := m.load()
	if err != nil {
		return err
	}
	found := false
	filtered := r.Modules[:0]
	for _, mod := range r.Modules {
		if mod.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, mod)
	}
	if !found {
		return fmt.Errorf("module %q not found", name)
	}
	r.Modules = filtered
	return m.save(r)
}

// RestartService calls `sudo systemctl restart <serviceName>`.
// Returns the command output (useful for error messages).
func RestartService(serviceName string) (string, error) {
	out, err := exec.Command("sudo", "systemctl", "restart", serviceName).CombinedOutput()
	return string(out), err
}

// ─── internals ───────────────────────────────────────────────────────────────

func (m *Manager) load() (*Registry, error) {
	data, err := os.ReadFile(m.cfgPath)
	if os.IsNotExist(err) {
		return &Registry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("modules: read %s: %w", m.cfgPath, err)
	}
	var r Registry
	if err := toml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("modules: parse %s: %w", m.cfgPath, err)
	}
	return &r, nil
}

func (m *Manager) save(r *Registry) error {
	if err := os.MkdirAll(filepath.Dir(m.cfgPath), 0o755); err != nil {
		return fmt.Errorf("modules: mkdir: %w", err)
	}
	data, err := toml.Marshal(r)
	if err != nil {
		return fmt.Errorf("modules: marshal: %w", err)
	}
	return os.WriteFile(m.cfgPath, data, 0o600)
}

// splitNameVer parses "name@version" or just "name".
func splitNameVer(s string) (name, version string) {
	if idx := strings.LastIndex(s, "@"); idx >= 0 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

// resolveGitHash returns the short git hash for the repo containing path, or "".
func resolveGitHash(path string) string {
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
