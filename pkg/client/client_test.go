package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethan-mdev/patchline/pkg/manifest"
	"github.com/ethan-mdev/patchline/pkg/publisher"
	"github.com/ethan-mdev/patchline/pkg/signing"
	localstorage "github.com/ethan-mdev/patchline/pkg/storage/local"
)

func TestClientPlansAndAppliesUpdate(t *testing.T) {
	ctx := context.Background()
	buildDir := t.TempDir()
	releaseDir := t.TempDir()
	installDir := t.TempDir()

	writeFile(t, filepath.Join(buildDir, "Game.bin"), "new-game")
	writeFile(t, filepath.Join(buildDir, "res", "ui", "hud.dat"), "hud")
	writeFile(t, filepath.Join(installDir, "Game.bin"), "old-game")
	signer, verifier := testSignerVerifier(t)

	if _, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Signer:          signer,
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.FileServer(http.Dir(releaseDir)))
	defer server.Close()

	c, err := New(Config{
		AppID:            "com.example.game",
		Channel:          "beta",
		BaseURL:          server.URL,
		InstallDir:       installDir,
		ManifestVerifier: verifier,
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
	signer, verifier := testSignerVerifier(t)

	if _, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "stable",
		ReleaseSequence: 3,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Signer:          signer,
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
		ManifestVerifier:    verifier,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.FetchChannelManifest(ctx); err == nil {
		t.Fatal("expected stale release sequence error")
	}
}

func TestFetchChannelManifestUsesVerifier(t *testing.T) {
	ctx := context.Background()
	buildDir := t.TempDir()
	releaseDir := t.TempDir()
	installDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "game")
	signer, _ := testSignerVerifier(t)

	if _, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Signer:          signer,
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(releaseDir)))
	defer server.Close()

	called := false
	c, err := New(Config{
		AppID:      "com.example.game",
		Channel:    "beta",
		BaseURL:    server.URL,
		InstallDir: installDir,
		ManifestVerifier: ManifestVerifierFunc(func(ctx context.Context, data []byte) error {
			called = true
			if len(data) == 0 {
				t.Fatal("verifier received empty manifest")
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.FetchChannelManifest(ctx); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected verifier to be called")
	}
}

func TestFetchChannelManifestStopsOnVerifierError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"not":"trusted"}`))
	}))
	defer server.Close()

	c, err := New(Config{
		AppID:      "com.example.game",
		Channel:    "beta",
		BaseURL:    server.URL,
		InstallDir: t.TempDir(),
		ManifestVerifier: ManifestVerifierFunc(func(ctx context.Context, data []byte) error {
			return context.Canceled
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.FetchChannelManifest(context.Background()); err == nil {
		t.Fatal("expected verifier error")
	}
}

func TestApplyRejectsHashMismatchWithoutReplacingExistingFile(t *testing.T) {
	ctx := context.Background()
	buildDir := t.TempDir()
	releaseDir := t.TempDir()
	installDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "new-game")
	writeFile(t, filepath.Join(installDir, "Game.bin"), "old-game")
	signer, verifier := testSignerVerifier(t)

	result, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Signer:          signer,
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
		AppID:            "com.example.game",
		Channel:          "beta",
		BaseURL:          server.URL,
		InstallDir:       installDir,
		ManifestVerifier: verifier,
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
	assertNoPatchlineTempFiles(t, installDir)
}

func TestFetchRejectsUnsafeManifestPath(t *testing.T) {
	key, err := manifest.ObjectKeyForHash("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	if err != nil {
		t.Fatal(err)
	}
	// Build a payload that bypasses manifest.Validate by writing the unsafe
	// path directly into the JSON. The client should reject it on decode.
	payload, err := json.Marshal(map[string]any{
		"format_version":   manifest.FormatVersion,
		"app_id":           "com.example.game",
		"version":          "1.0.0",
		"channel":          "beta",
		"release_sequence": 1,
		"published_at":     time.Unix(100, 0).UTC(),
		"files": []map[string]any{{
			"path":       "../Game.bin",
			"size":       5,
			"sha256":     "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			"object_key": key,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := manifest.EncodeEnvelope(payload, nil)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(envelope)
	}))
	defer server.Close()

	c, err := New(Config{
		AppID:            "com.example.game",
		Channel:          "beta",
		BaseURL:          server.URL,
		InstallDir:       t.TempDir(),
		AllowUnsignedDev: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.FetchChannelManifest(context.Background()); err == nil {
		t.Fatal("expected unsafe path error")
	}
}

func TestApplyDownloadFailureKeepsExistingFileAndCleansTempFile(t *testing.T) {
	ctx := context.Background()
	buildDir := t.TempDir()
	releaseDir := t.TempDir()
	installDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "new-game")
	writeFile(t, filepath.Join(installDir, "Game.bin"), "old-game")
	signer, verifier := testSignerVerifier(t)

	if _, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Signer:          signer,
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/objects/") {
			http.NotFound(w, r)
			return
		}
		http.FileServer(http.Dir(releaseDir)).ServeHTTP(w, r)
	}))
	defer server.Close()

	c, err := New(Config{
		AppID:            "com.example.game",
		Channel:          "beta",
		BaseURL:          server.URL,
		InstallDir:       installDir,
		ManifestVerifier: verifier,
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
		t.Fatal("expected download failure")
	}
	assertFileContent(t, filepath.Join(installDir, "Game.bin"), "old-game")
	assertNoPatchlineTempFiles(t, installDir)
}

func TestApplyCreatesInstallDirectory(t *testing.T) {
	ctx := context.Background()
	buildDir := t.TempDir()
	releaseDir := t.TempDir()
	installRoot := t.TempDir()
	installDir := filepath.Join(installRoot, "missing-install")
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "game")
	signer, verifier := testSignerVerifier(t)

	if _, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Signer:          signer,
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.FileServer(http.Dir(releaseDir)))
	defer server.Close()

	c, err := New(Config{
		AppID:            "com.example.game",
		Channel:          "beta",
		BaseURL:          server.URL,
		InstallDir:       installDir,
		ManifestVerifier: verifier,
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
	if err := c.Apply(ctx, plan); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(installDir, "Game.bin"), "game")
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

func assertNoPatchlineTempFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".patchline-download-") {
			t.Fatalf("temporary file was not cleaned up: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testSignerVerifier(t *testing.T) (*signing.Signer, *signing.Verifier) {
	t.Helper()
	pair, err := signing.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := signing.NewSigner(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := signing.NewVerifier(pair.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer, verifier
}
