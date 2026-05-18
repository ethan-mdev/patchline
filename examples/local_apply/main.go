package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/ethan-mdev/patchline/pkg/client"
	"github.com/ethan-mdev/patchline/pkg/publisher"
	localstorage "github.com/ethan-mdev/patchline/pkg/storage/local"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	root, err := os.MkdirTemp("", "patchline-local-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	buildDir := filepath.Join(root, "build")
	releaseDir := filepath.Join(root, "release")
	installDir := filepath.Join(root, "install")

	if err := writeFile(filepath.Join(buildDir, "Game.bin"), "new-game"); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(buildDir, "res", "ui", "hud.dat"), "hud"); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(installDir, "Game.bin"), "old-game"); err != nil {
		return err
	}

	result, err := publisher.Publish(ctx, localstorage.New(releaseDir), buildDir, publisher.Options{
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
	})
	if err != nil {
		return err
	}

	server := httptest.NewServer(http.FileServer(http.Dir(releaseDir)))
	defer server.Close()

	c, err := client.New(client.Config{
		AppID:      "com.example.game",
		Channel:    "beta",
		BaseURL:    server.URL,
		InstallDir: installDir,
	})
	if err != nil {
		return err
	}

	m, err := c.FetchChannelManifest(ctx)
	if err != nil {
		return err
	}
	plan, err := c.Plan(ctx, m)
	if err != nil {
		return err
	}
	if err := c.Apply(ctx, plan); err != nil {
		return err
	}
	updated, err := os.ReadFile(filepath.Join(installDir, "Game.bin"))
	if err != nil {
		return err
	}

	fmt.Printf("published %s with %d files\n", result.Manifest.Version, len(result.Manifest.Files))
	fmt.Printf("applied %d files (%d bytes)\n", len(plan.Files), plan.TotalBytes)
	fmt.Printf("Game.bin now contains %q\n", string(updated))
	return nil
}

func writeFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
