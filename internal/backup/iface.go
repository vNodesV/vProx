package backup

// Manager is the interface for backup operations, enabling dependency
// injection and testing without touching the filesystem.
//
// The default implementation uses the package-level RunOnce and StartAuto
// functions. Test code can substitute a mock Manager.
type Manager interface {
	// RunOnce performs a single backup run with the given options.
	RunOnce(opts Options) error

	// StartAuto starts an automated backup loop. Returns a stop function.
	StartAuto(opts Options) (stop func(), err error)

	// LoadConfig loads backup.toml from path.
	// Returns (cfg, loaded, err) where loaded is true if file was read.
	LoadConfig(path string) (BackupConfig, bool, error)
}
