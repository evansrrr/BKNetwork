package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	var requestedTarget string
	var requestedVersion string
	flag.StringVar(&requestedTarget, "target", "", "Rust target triple (defaults to BKNETWORK_TARGET_TRIPLE or rustc host)")
	flag.StringVar(&requestedVersion, "version", "", "application version (defaults to Cargo.toml)")
	flag.Parse()

	repoRoot, err := findRepoRoot()
	check(err)

	target := strings.TrimSpace(requestedTarget)
	if target == "" {
		target = strings.TrimSpace(os.Getenv("BKNETWORK_TARGET_TRIPLE"))
	}
	if target == "" {
		target, err = commandOutput(repoRoot, "rustc", "--print", "host-tuple")
		check(err)
	}

	version := strings.TrimSpace(requestedVersion)
	if version == "" {
		version, err = cargoPackageVersion(filepath.Join(repoRoot, "src-tauri", "Cargo.toml"))
		check(err)
	}

	goos, goarch, extension, err := goTarget(target)
	check(err)
	binaryDir := filepath.Join(repoRoot, "src-tauri", "binaries")
	check(os.MkdirAll(binaryDir, 0o755))
	outputPath := filepath.Join(binaryDir, "bknetwork-server-"+target+extension)

	ldflags := "-s -w -X bknetwork/internal/buildinfo.Version=" + version
	if goos == "windows" {
		ldflags += " -H=windowsgui"
	}

	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", outputPath, "./cmd/bknetwork-sidecar")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	check(cmd.Run())

	fmt.Printf("Tauri sidecar built: %s (version %s)\n", outputPath, version)
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from current directory")
		}
		dir = parent
	}
}

func commandOutput(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func cargoPackageVersion(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	inPackage := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inPackage = line == "[package]"
			continue
		}
		if !inPackage || !strings.HasPrefix(line, "version") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "version" {
			version := strings.Trim(strings.TrimSpace(value), "\"")
			if version != "" {
				return version, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("package version not found in %s", path)
}

func goTarget(target string) (goos, goarch, extension string, err error) {
	switch target {
	case "x86_64-pc-windows-msvc":
		return "windows", "amd64", ".exe", nil
	case "aarch64-apple-darwin":
		return "darwin", "arm64", "", nil
	case "x86_64-apple-darwin":
		return "darwin", "amd64", "", nil
	default:
		return "", "", "", fmt.Errorf("unsupported Tauri target: %s", target)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
