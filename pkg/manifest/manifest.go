package manifest

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const FormatVersion = 1

var (
	appIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	channelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

type Manifest struct {
	FormatVersion   int            `json:"format_version"`
	AppID           string         `json:"app_id"`
	Version         string         `json:"version"`
	Channel         string         `json:"channel"`
	ReleaseSequence int64          `json:"release_sequence"`
	PublishedAt     time.Time      `json:"published_at"`
	Files           []File         `json:"files"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	Signature       *Signature     `json:"signature,omitempty"`
}

type File struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	ObjectKey string `json:"object_key"`
}

type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id,omitempty"`
	Value     string `json:"value"`
}

type ManifestRef struct {
	Version         string    `json:"version"`
	Channel         string    `json:"channel,omitempty"`
	ReleaseSequence int64     `json:"release_sequence"`
	PublishedAt     time.Time `json:"published_at"`
}

func ObjectKeyForHash(sha256 string) (string, error) {
	hash := strings.ToLower(strings.TrimSpace(sha256))
	if !IsSHA256(hash) {
		return "", fmt.Errorf("invalid sha256 hash %q", sha256)
	}
	return fmt.Sprintf("objects/sha256/%s/%s/%s", hash[:2], hash[2:4], hash), nil
}

func IsSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func Validate(m *Manifest) error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if m.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported format_version %d", m.FormatVersion)
	}
	if !appIDPattern.MatchString(m.AppID) {
		return fmt.Errorf("invalid app_id %q", m.AppID)
	}
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("invalid version %q", m.Version)
	}
	if !channelPattern.MatchString(m.Channel) {
		return fmt.Errorf("invalid channel %q", m.Channel)
	}
	if m.ReleaseSequence < 1 {
		return errors.New("release_sequence must be positive")
	}
	if m.PublishedAt.IsZero() {
		return errors.New("published_at is required")
	}

	seen := make(map[string]struct{}, len(m.Files))
	for _, file := range m.Files {
		if strings.TrimSpace(file.Path) == "" {
			return errors.New("file path is required")
		}
		if _, exists := seen[file.Path]; exists {
			return fmt.Errorf("duplicate file path %q", file.Path)
		}
		seen[file.Path] = struct{}{}

		if file.Size < 0 {
			return fmt.Errorf("file %q has negative size", file.Path)
		}
		if !IsSHA256(file.SHA256) {
			return fmt.Errorf("file %q has invalid sha256", file.Path)
		}
		expectedKey, err := ObjectKeyForHash(file.SHA256)
		if err != nil {
			return err
		}
		if file.ObjectKey != expectedKey {
			return fmt.Errorf("file %q object_key = %q, want %q", file.Path, file.ObjectKey, expectedKey)
		}
	}
	return nil
}
