package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "BKNetwork", "settings.json")

	want := Settings{AutoStart: true, SilentStart: true, WarpAutoStart: true, WarpAppAutoStart: false}
	if err := saveTo(path, want); err != nil {
		t.Fatalf("saveTo: %v", err)
	}

	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got.AutoStart != want.AutoStart || got.SilentStart != want.SilentStart ||
		got.WarpAutoStart != want.WarpAutoStart || got.WarpAppAutoStart != want.WarpAppAutoStart {
		t.Fatalf("loaded settings = %+v, want %+v", got, want)
	}
	if got.SchemaVersion != currentSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "settings.json")
	if err := saveTo(path, Settings{AutoStart: true}); err != nil {
		t.Fatalf("saveTo: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file was not created: %v", err)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got.AutoStart || got.WarpAutoStart {
		t.Fatalf("defaults = %+v, want everything false", got)
	}
	if got.SchemaVersion != currentSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
}

// TestLoadLegacyFileWithoutVersion covers an existing settings.json written by
// a release that did not have the field yet.
func TestLoadLegacyFileWithoutVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"autoStart":true,"warpAutoStart":true}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if !got.AutoStart || !got.WarpAutoStart {
		t.Fatalf("loaded settings = %+v, want the stored values", got)
	}
	if got.SchemaVersion != currentSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
}

func TestLoadRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"autoStart": tru`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := loadFrom(path); err == nil {
		t.Fatal("expected an error for a truncated settings file")
	}
}

// TestSaveLeavesNoTemporaryFiles guards the atomic write path: the temporary
// file must always be renamed away or removed.
func TestSaveLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	for i := 0; i < 3; i++ {
		if err := saveTo(path, Settings{AutoStart: i%2 == 0}); err != nil {
			t.Fatalf("saveTo: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var leftovers []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".settings-") {
			leftovers = append(leftovers, entry.Name())
		}
	}
	if len(leftovers) > 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestSaveOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	if err := saveTo(path, Settings{AutoStart: true, SilentStart: true}); err != nil {
		t.Fatalf("first saveTo: %v", err)
	}
	if err := saveTo(path, Settings{AutoStart: false}); err != nil {
		t.Fatalf("second saveTo: %v", err)
	}

	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got.AutoStart || got.SilentStart {
		t.Fatalf("settings = %+v, want the second write", got)
	}
}
