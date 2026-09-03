package main

import "testing"

func TestGoTarget(t *testing.T) {
	tests := []struct {
		target    string
		goos      string
		goarch    string
		extension string
	}{
		{target: "x86_64-pc-windows-msvc", goos: "windows", goarch: "amd64", extension: ".exe"},
		{target: "aarch64-apple-darwin", goos: "darwin", goarch: "arm64"},
		{target: "x86_64-apple-darwin", goos: "darwin", goarch: "amd64"},
	}

	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			goos, goarch, extension, err := goTarget(test.target)
			if err != nil {
				t.Fatalf("goTarget() error = %v", err)
			}
			if goos != test.goos || goarch != test.goarch || extension != test.extension {
				t.Fatalf("goTarget() = %s/%s %q, want %s/%s %q", goos, goarch, extension, test.goos, test.goarch, test.extension)
			}
		})
	}
}

func TestGoTargetRejectsUnsupportedTarget(t *testing.T) {
	if _, _, _, err := goTarget("aarch64-pc-windows-msvc"); err == nil {
		t.Fatal("goTarget() accepted an unsupported target")
	}
}
