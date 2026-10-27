package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// currentSchemaVersion is written on every save. Files without the field come
// from releases before it existed and are treated as the current version.
const currentSchemaVersion = 1

type Settings struct {
	SchemaVersion    int  `json:"schemaVersion,omitempty"`
	AutoStart        bool `json:"autoStart"`
	SilentStart      bool `json:"silentStart"`
	WarpAutoStart    bool `json:"warpAutoStart"`
	WarpAppAutoStart bool `json:"warpAppAutoStart"`
	// DisableOnlineChecks turns off the outbound requests the app makes on its
	// own (GitHub version check, public IPv6 echo). Campus and restricted
	// networks block or throttle them, and a failing check only produces noise.
	DisableOnlineChecks bool `json:"disableOnlineChecks"`
}

var mu sync.Mutex

func Load() (Settings, error) {
	path, err := configPath()
	if err != nil {
		return Settings{}, err
	}
	return loadFrom(path)
}

func Save(cfg Settings) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return saveTo(path, cfg)
}

func loadFrom(path string) (Settings, error) {
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// First run: the caller gets defaults instead of an error.
			return Settings{SchemaVersion: currentSchemaVersion}, nil
		}
		return Settings{}, err
	}

	var cfg Settings
	if err := json.Unmarshal(data, &cfg); err != nil {
		// A truncated file used to be silently replaced by defaults, which made
		// settings disappear. Report it so the caller can decide.
		return Settings{}, fmt.Errorf("parse settings file %s: %w", path, err)
	}
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = currentSchemaVersion
	}
	return cfg, nil
}

func saveTo(path string, cfg Settings) error {
	mu.Lock()
	defer mu.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	cfg.SchemaVersion = currentSchemaVersion
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// writeFileAtomic writes data to a temporary file in the same directory and
// renames it over path, so a crash or power loss can never leave a half written
// settings file behind.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Harmless after a successful rename, required on every error path.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "BKNetwork", "settings.json"), nil
}
