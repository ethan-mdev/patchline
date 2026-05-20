package manifest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
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

// Envelope is the on-disk and on-wire wrapper around a manifest. The signature
// is verified against the literal bytes of Payload, never against bytes
// produced by re-marshaling a decoded Manifest struct. This means unknown
// future fields in the payload are preserved through signing and verification.
type Envelope struct {
	Payload   json.RawMessage `json:"payload"`
	Signature *Signature      `json:"signature,omitempty"`
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
		if err := validateFilePath(file.Path); err != nil {
			return err
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

// EncodePayload validates the manifest and returns its canonical JSON bytes.
// These are the bytes that get signed and that the verifier checks against.
func EncodePayload(m *Manifest) ([]byte, error) {
	if err := Validate(m); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// EncodeEnvelope returns the envelope bytes ready to write to storage. The
// payload bytes are embedded verbatim so the signature stays valid; the
// surrounding envelope keys are pretty-printed for readability. We can't use
// json.MarshalIndent on a struct containing the payload as json.RawMessage
// because the indenter would re-format the payload's interior whitespace.
func EncodeEnvelope(payload []byte, sig *Signature) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("payload is empty")
	}
	if !json.Valid(payload) {
		return nil, errors.New("payload is not valid JSON")
	}

	sigBytes := []byte("null")
	if sig != nil {
		encoded, err := json.MarshalIndent(sig, "  ", "  ")
		if err != nil {
			return nil, err
		}
		sigBytes = encoded
	}

	var buf bytes.Buffer
	buf.WriteString("{\n  \"payload\": ")
	buf.Write(payload)
	buf.WriteString(",\n  \"signature\": ")
	buf.Write(sigBytes)
	buf.WriteString("\n}")
	return buf.Bytes(), nil
}

// DecodeEnvelope parses envelope bytes and returns the raw payload bytes and
// the signature, if any. Payload bytes are returned verbatim so the verifier
// can check them against the signature without re-serializing the manifest.
func DecodeEnvelope(data []byte) ([]byte, *Signature, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, nil, fmt.Errorf("decode envelope: %w", err)
	}
	if len(env.Payload) == 0 {
		return nil, nil, errors.New("envelope payload is empty")
	}
	return []byte(env.Payload), env.Signature, nil
}

// DecodeManifest parses payload bytes into a manifest and validates it.
func DecodeManifest(payload []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := Validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func validateFilePath(filePath string) error {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return errors.New("file path is required")
	}
	if trimmed != filePath {
		return fmt.Errorf("file path %q must not contain leading or trailing whitespace", filePath)
	}
	if strings.Contains(filePath, "\\") {
		return fmt.Errorf("file path %q must use slash separators", filePath)
	}
	if strings.HasPrefix(filePath, "/") {
		return fmt.Errorf("file path %q must be relative", filePath)
	}

	cleaned := path.Clean(filePath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("file path %q must stay within the install root", filePath)
	}
	if cleaned != filePath {
		return fmt.Errorf("file path %q must be clean", filePath)
	}
	return nil
}
