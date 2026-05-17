package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethan-mdev/dispatch/pkg/publisher"
	localstorage "github.com/ethan-mdev/dispatch/pkg/storage/local"
)

func TestClientPlansAndAppliesUpdate(t *testing.T) {
	ctx := context.Background()
	buildDir := t.TempDir()
	releaseDir := t.TempDir()
	installDir := t.TempDir()

	writeFile(t, filepath.Join(buildDir, "Game.bin"), "new-game")
	writeFile(t, filepath.Join(buildDir, "res", "ui", "hud.dat"), "hud")
	writeFile(t, filepath.Join(installDir, "Game.bin"), "old-game")

	if _, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.FileServer(http.Dir(releaseDir)))
	defer server.Close()

	c, err := New(Config{
		AppID:      "com.example.game",
		Channel:    "beta",
		BaseURL:    server.URL,
		InstallDir: installDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := c.FetchChannelManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.Plan(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 2 || plan.TotalBytes != int64(len("new-game")+len("hud")) {
		t.Fatalf("plan = %#v", plan)
	}

	if err := c.Apply(ctx, plan); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(installDir, "Game.bin"), "new-game")
	assertFileContent(t, filepath.Join(installDir, "res", "ui", "hud.dat"), "hud")

	plan, err = c.Plan(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 0 || plan.TotalBytes != 0 {
		t.Fatalf("post-apply plan = %#v, want empty", plan)
	}
}

func TestClientRejectsOldReleaseSequence(t *testing.T) {
	ctx := context.Background()
	buildDir := t.TempDir()
	releaseDir := t.TempDir()
	installDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "game")

	if _, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "stable",
		ReleaseSequence: 3,
		PublishedAt:     time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(releaseDir)))
	defer server.Close()

	c, err := New(Config{
		AppID:               "com.example.game",
		Channel:             "stable",
		BaseURL:             server.URL,
		InstallDir:          installDir,
		LastReleaseSequence: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.FetchChannelManifest(ctx); err == nil {
		t.Fatal("expected stale release sequence error")
	}
}

func TestApplyRejectsHashMismatchWithoutReplacingExistingFile(t *testing.T) {
	ctx := context.Background()
	buildDir := t.TempDir()
	releaseDir := t.TempDir()
	installDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "new-game")
	writeFile(t, filepath.Join(installDir, "Game.bin"), "old-game")

	result, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	badObject := "/" + result.Manifest.Files[0].ObjectKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == badObject {
			_, _ = w.Write([]byte("bad-game"))
			return
		}
		http.FileServer(http.Dir(releaseDir)).ServeHTTP(w, r)
	}))
	defer server.Close()

	c, err := New(Config{
		AppID:      "com.example.game",
		Channel:    "beta",
		BaseURL:    server.URL,
		InstallDir: installDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := c.FetchChannelManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.Plan(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Apply(ctx, plan); err == nil {
		t.Fatal("expected hash mismatch")
	}
	assertFileContent(t, filepath.Join(installDir, "Game.bin"), "old-game")
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

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}
