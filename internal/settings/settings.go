package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Settings struct {
	AutoStart        bool `json:"autoStart"`
	SilentStart      bool `json:"silentStart"`
	WarpAutoStart    bool `json:"warpAutoStart"`
	WarpAppAutoStart bool `json:"warpAppAutoStart"`
}

var mu sync.Mutex

func Load() (Settings, error) {
	mu.Lock()
	defer mu.Unlock()

	path, err := configPath()
	if err != nil {
		return Settings{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	var cfg Settings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Settings{}, err
	}
	return cfg, nil
}

func Save(cfg Settings) error {
	mu.Lock()
	defer mu.Unlock()

	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "BKNetwork", "settings.json"), nil
}
