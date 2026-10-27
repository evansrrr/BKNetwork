package buildinfo

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func TestVersionIsSemver(t *testing.T) {
	if !semverPattern.MatchString(Version) {
		t.Fatalf("Version = %q, want a x.y.z version", Version)
	}
}

// TestVersionMatchesDesktopPackage keeps the Go version and the Tauri version
// from drifting apart: the UI reads the Go value while the installer, the
// bundle and the release tag check use the Cargo value.
func TestVersionMatchesDesktopPackage(t *testing.T) {
	path := filepath.Join("..", "..", "src-tauri", "Cargo.toml")
	file, err := os.Open(path)
	if err != nil {
		t.Skipf("desktop package manifest not available: %v", err)
	}
	defer file.Close()

	version, err := packageVersion(file)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if version != Version {
		t.Fatalf("src-tauri/Cargo.toml says %q but buildinfo.Version is %q", version, Version)
	}
}

// TestVersionMatchesShellMarkup covers the version the page shows before the
// first status response arrives: index.html carries a placeholder that the UI
// replaces from the API, so it has to be bumped with the rest.
func TestVersionMatchesShellMarkup(t *testing.T) {
	path := filepath.Join("..", "..", "web", "index.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("web shell not available: %v", err)
	}
	want := ">v" + Version + "<"
	if !bytes.Contains(data, []byte(want)) {
		t.Fatalf("%s does not contain %q; bump the version placeholder together with buildinfo.Version", path, want)
	}
}

// packageVersion returns the version of the [package] section.
func packageVersion(file *os.File) (string, error) {
	scanner := bufio.NewScanner(file)
	inPackage := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inPackage = line == "[package]"
			continue
		}
		if !inPackage || !strings.HasPrefix(line, "version") {
			continue
		}
		if idx := strings.Index(line, "\""); idx >= 0 {
			rest := line[idx+1:]
			if end := strings.Index(rest, "\""); end >= 0 {
				return rest[:end], nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", os.ErrNotExist
}
