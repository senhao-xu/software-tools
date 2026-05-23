package network

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChooseSource(t *testing.T) {
	const onlineURL = "https://example.com/manifest.yml"

	t.Run("empty assets dir returns online", func(t *testing.T) {
		got := chooseSource("", "manifest.yml", onlineURL)
		if got != onlineURL {
			t.Errorf("chooseSource(\"\", ...) = %q, want %q", got, onlineURL)
		}
	})

	t.Run("file exists returns local path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.yml")
		if err := os.WriteFile(path, []byte("kind: Pod\n"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		got := chooseSource(dir, "manifest.yml", onlineURL)
		if got != path {
			t.Errorf("chooseSource(dir, ...) = %q, want %q", got, path)
		}
	})

	t.Run("file missing returns online", func(t *testing.T) {
		dir := t.TempDir()
		got := chooseSource(dir, "manifest.yml", onlineURL)
		if got != onlineURL {
			t.Errorf("chooseSource(empty-dir, ...) = %q, want %q", got, onlineURL)
		}
	})

	t.Run("path is a directory returns online", func(t *testing.T) {
		dir := t.TempDir()
		// Create a subdirectory named like the asset; chooseSource should
		// reject it as not-a-regular-file and fall back to online.
		if err := os.Mkdir(filepath.Join(dir, "manifest.yml"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		got := chooseSource(dir, "manifest.yml", onlineURL)
		if got != onlineURL {
			t.Errorf("chooseSource(dir-with-dir-asset, ...) = %q, want %q", got, onlineURL)
		}
	})
}
