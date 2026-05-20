package releaseops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethan-mdev/patchline/pkg/publisher"
	"github.com/ethan-mdev/patchline/pkg/signing"
	localstorage "github.com/ethan-mdev/patchline/pkg/storage/local"
)

func TestVerifyChecksSignedManifestAndObjects(t *testing.T) {
	ctx := context.Background()
	backend, _, verifier := publishTestRelease(t, "1.0.0", "beta", "game-v1")

	result, err := Verify(ctx, VerifyOptions{
		Backend:  backend,
		Version:  "1.0.0",
		Verifier: verifier,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", result.Manifest.Version)
	}
	if result.ObjectsChecked != 1 {
		t.Fatalf("objects checked = %d, want 1", result.ObjectsChecked)
	}
}

func TestVerifyRequiresVerifierUnlessUnsignedDev(t *testing.T) {
	ctx := context.Background()
	backend, _, _ := publishTestRelease(t, "1.0.0", "beta", "game-v1")

	if _, err := Verify(ctx, VerifyOptions{Backend: backend, Channel: "beta"}); err == nil {
		t.Fatal("expected missing verifier error")
	}
	if _, err := Verify(ctx, VerifyOptions{Backend: backend, Channel: "beta", AllowUnsignedDev: true}); err != nil {
		t.Fatal(err)
	}
}

func TestPromoteWritesSignedChannelManifest(t *testing.T) {
	ctx := context.Background()
	backend, signer, verifier := publishTestRelease(t, "1.0.0", "beta", "game-v1")

	result, err := Promote(ctx, MoveOptions{
		Backend:     backend,
		Version:     "1.0.0",
		Channel:     "stable",
		Signer:      signer,
		PublishedAt: time.Unix(200, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "promote" {
		t.Fatalf("action = %q, want promote", result.Action)
	}
	if result.Manifest.Channel != "stable" || result.Manifest.ReleaseSequence != 1 {
		t.Fatalf("promoted manifest = %#v", result.Manifest)
	}

	if _, err := Verify(ctx, VerifyOptions{
		Backend:  backend,
		Channel:  "stable",
		Verifier: verifier,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRollbackIncrementsExistingChannelSequence(t *testing.T) {
	ctx := context.Background()
	backend, signer, verifier := publishTestRelease(t, "1.0.0", "stable", "game-v1")
	buildDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), "game-v2")
	if _, err := publisher.Publish(ctx, backend, buildDir, publisher.Options{
		AppID:       "com.example.game",
		Version:     "1.0.1",
		Channel:     "stable",
		PublishedAt: time.Unix(200, 0).UTC(),
		Signer:      signer,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := Rollback(ctx, MoveOptions{
		Backend:     backend,
		Version:     "1.0.0",
		Channel:     "stable",
		Signer:      signer,
		PublishedAt: time.Unix(300, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "rollback" {
		t.Fatalf("action = %q, want rollback", result.Action)
	}
	if result.Manifest.Version != "1.0.0" || result.Manifest.ReleaseSequence != 3 {
		t.Fatalf("rollback manifest version=%q sequence=%d, want 1.0.0 sequence=3", result.Manifest.Version, result.Manifest.ReleaseSequence)
	}
	if _, err := Verify(ctx, VerifyOptions{Backend: backend, Channel: "stable", Verifier: verifier}); err != nil {
		t.Fatal(err)
	}
}

func TestGCDryRunAndDeleteUnreferencedObjects(t *testing.T) {
	ctx := context.Background()
	backend, _, _ := publishTestRelease(t, "1.0.0", "beta", "game-v1")
	orphanHash := sha256Hex("orphan")
	if err := backend.PutObject(ctx, orphanHash, bytes.NewBufferString("orphan")); err != nil {
		t.Fatal(err)
	}

	dryRun, err := GC(ctx, GCOptions{Backend: backend, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(dryRun.Deleted) != 1 || dryRun.Deleted[0] != orphanHash {
		t.Fatalf("dry run deleted = %#v, want %s", dryRun.Deleted, orphanHash)
	}
	exists, err := backend.ObjectExists(ctx, orphanHash)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("dry run should not delete the orphan object")
	}

	result, err := GC(ctx, GCOptions{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != orphanHash {
		t.Fatalf("deleted = %#v, want %s", result.Deleted, orphanHash)
	}
	exists, err = backend.ObjectExists(ctx, orphanHash)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("orphan object still exists after gc")
	}
}

func TestDoctorReportsStorageChecks(t *testing.T) {
	backend, _, _ := publishTestRelease(t, "1.0.0", "beta", "game-v1")

	result, err := Doctor(context.Background(), DoctorOptions{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("doctor result = %#v, want ok", result)
	}
	if len(result.Checks) != 3 {
		t.Fatalf("checks = %#v, want object/release/channel checks", result.Checks)
	}
}

func publishTestRelease(t *testing.T, version string, channel string, content string) (*localstorage.Backend, *signing.Signer, *signing.Verifier) {
	t.Helper()
	buildDir := t.TempDir()
	writeFile(t, filepath.Join(buildDir, "Game.bin"), content)
	backend := localstorage.New(t.TempDir())
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
	if _, err := publisher.Publish(context.Background(), backend, buildDir, publisher.Options{
		AppID:       "com.example.game",
		Version:     version,
		Channel:     channel,
		PublishedAt: time.Unix(100, 0).UTC(),
		Signer:      signer,
	}); err != nil {
		t.Fatal(err)
	}
	return backend, signer, verifier
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

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}
