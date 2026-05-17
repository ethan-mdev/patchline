package manifest

import (
	"strings"
	"testing"
	"time"
)

const testHash = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestObjectKeyForHash(t *testing.T) {
	key, err := ObjectKeyForHash(strings.ToUpper(testHash))
	if err != nil {
		t.Fatal(err)
	}
	want := "objects/sha256/2c/f2/" + testHash
	if key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
}

func TestValidateManifest(t *testing.T) {
	key, err := ObjectKeyForHash(testHash)
	if err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		FormatVersion:   FormatVersion,
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Files: []File{{
			Path:      "bin/game.exe",
			Size:      5,
			SHA256:    testHash,
			ObjectKey: key,
		}},
	}

	if err := Validate(m); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsDuplicatePath(t *testing.T) {
	key, err := ObjectKeyForHash(testHash)
	if err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		FormatVersion:   FormatVersion,
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "stable",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Files: []File{
			{Path: "game.exe", Size: 5, SHA256: testHash, ObjectKey: key},
			{Path: "game.exe", Size: 5, SHA256: testHash, ObjectKey: key},
		},
	}

	if err := Validate(m); err == nil {
		t.Fatal("expected duplicate path error")
	}
}
