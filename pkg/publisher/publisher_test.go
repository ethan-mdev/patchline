package publisher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethan-mdev/patchline/pkg/manifest"
	localstorage "github.com/ethan-mdev/patchline/pkg/storage/local"
)

func TestPublishWritesLocalContentAddressedRelease(t *testing.T) {
	buildDir := t.TempDir()
	outputDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "game")
	writeFile(t, filepath.Join(buildDir, "res", "ui", "hud.dat"), "hud")

	result, err := Publish(context.Background(), localstorage.New(outputDir), buildDir, Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.ObjectsUploaded != 2 || result.ObjectsReused != 0 {
		t.Fatalf("uploads=%d reused=%d, want uploads=2 reused=0", result.ObjectsUploaded, result.ObjectsReused)
	}
	if len(result.Manifest.Files) != 2 {
		t.Fatalf("manifest files = %#v", result.Manifest.Files)
	}
	if result.Manifest.Files[0].Path != "Game.bin" || result.Manifest.Files[1].Path != "res/ui/hud.dat" {
		t.Fatalf("files not sorted: %#v", result.Manifest.Files)
	}

	assertFileExists(t, filepath.Join(outputDir, "releases", "1.0.0", "manifest.json"))
	assertFileExists(t, filepath.Join(outputDir, "channels", "beta", "manifest.json"))
	for _, file := range result.Manifest.Files {
		assertFileExists(t, filepath.Join(outputDir, filepath.FromSlash(file.ObjectKey)))
	}

	channelManifest := readManifest(t, filepath.Join(outputDir, "channels", "beta", "manifest.json"))
	if channelManifest.Version != "1.0.0" || channelManifest.ReleaseSequence != 1 {
		t.Fatalf("channel manifest = %#v", channelManifest)
	}
}

func TestPublishReusesExistingObjectsAndIncrementsSequence(t *testing.T) {
	buildDir := t.TempDir()
	outputDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "game")

	backend := localstorage.New(outputDir)
	if _, err := Publish(context.Background(), backend, buildDir, Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "stable",
		ReleaseSequence: 7,
		PublishedAt:     time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := Publish(context.Background(), backend, buildDir, Options{
		AppID:       "com.example.game",
		Version:     "1.0.1",
		Channel:     "stable",
		PublishedAt: time.Unix(200, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.ObjectsUploaded != 0 || result.ObjectsReused != 1 {
		t.Fatalf("uploads=%d reused=%d, want uploads=0 reused=1", result.ObjectsUploaded, result.ObjectsReused)
	}
	if result.Manifest.ReleaseSequence != 8 {
		t.Fatalf("release_sequence = %d, want 8", result.Manifest.ReleaseSequence)
	}
}

func readManifest(t *testing.T, path string) manifest.Manifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
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
