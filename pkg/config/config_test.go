package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesYAMLAndInterpolatesEnv(t *testing.T) {
	t.Setenv("PATCHLINE_SIGNING_KEY", "/secrets/patchline.key")

	dir := t.TempDir()
	path := filepath.Join(dir, "patchline.yaml")
	body := `
app_id: com.example.game
channel: beta
base_url: https://updates.example.com
install_dir: ./install
signing_key: ${PATCHLINE_SIGNING_KEY}
public_key: ./patchline.pub
backend:
  type: s3
  bucket: my-game-releases
  region: us-west-2
  prefix: releases/
  force_path_style: true
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppID != "com.example.game" {
		t.Fatalf("app_id = %q", cfg.AppID)
	}
	if cfg.SigningKey != "/secrets/patchline.key" {
		t.Fatalf("signing_key = %q, want env-expanded path", cfg.SigningKey)
	}
	if cfg.Backend.Type != "s3" || cfg.Backend.Bucket != "my-game-releases" {
		t.Fatalf("backend = %#v", cfg.Backend)
	}
	if !cfg.Backend.ForcePathStyle {
		t.Fatal("force_path_style should be true")
	}
}

func TestLoadReturnsEmptyConfigWhenMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.AppID != "" {
		t.Fatalf("expected empty config, got %#v", cfg)
	}
}

func TestLoadEmptyPathReturnsEmptyConfig(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.Backend.Type != "" {
		t.Fatalf("expected empty config, got %#v", cfg)
	}
}

func TestInterpolateEnvIgnoresUnmatchedDollar(t *testing.T) {
	t.Setenv("VAR", "value")
	if got := interpolateEnv("price: $25"); got != "price: $25" {
		t.Fatalf("interpolateEnv mangled $25: %q", got)
	}
	if got := interpolateEnv("path: ${VAR}/bin"); got != "path: value/bin" {
		t.Fatalf("interpolateEnv = %q", got)
	}
	if got := interpolateEnv("path: ${MISSING}/bin"); got != "path: /bin" {
		t.Fatalf("missing env should expand to empty, got %q", got)
	}
}
