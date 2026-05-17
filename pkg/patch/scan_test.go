package patch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanDirScansNestedFilesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Game.bin"), "game")
	writeFile(t, filepath.Join(root, "res", "ui", "hud.dat"), "hud")
	writeFile(t, filepath.Join(root, "manifest.json"), "old")

	files, err := ScanDir(context.Background(), root, ScanOptions{
		ExcludeNames: map[string]bool{"manifest.json": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %#v, want 2", files)
	}
	if files[0].Path != "Game.bin" {
		t.Fatalf("files[0].Path = %q, want Game.bin", files[0].Path)
	}
	if files[1].Path != "res/ui/hud.dat" {
		t.Fatalf("files[1].Path = %q, want res/ui/hud.dat", files[1].Path)
	}
	if files[1].SHA256 == "" {
		t.Fatal("expected hash")
	}
}

func TestCleanRelativePathRejectsTraversal(t *testing.T) {
	invalid := []string{"", "/abs", "../secret", "bin/../../secret"}
	for _, path := range invalid {
		if _, err := CleanRelativePath(path); err == nil {
			t.Fatalf("CleanRelativePath(%q) returned nil error", path)
		}
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
