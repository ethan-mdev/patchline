package releaseops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/ethan-mdev/patchline/pkg/manifest"
	"github.com/ethan-mdev/patchline/pkg/publisher"
	"github.com/ethan-mdev/patchline/pkg/storage"
)

type ManifestVerifier interface {
	VerifyManifest(ctx context.Context, data []byte) error
}

type VerifyOptions struct {
	Backend          storage.Backend
	Version          string
	Channel          string
	Verifier         ManifestVerifier
	AllowUnsignedDev bool
}

type VerifyResult struct {
	Manifest       *manifest.Manifest `json:"manifest"`
	ObjectsChecked int                `json:"objects_checked"`
}

type MoveOptions struct {
	Backend      storage.Backend
	Version      string
	Channel      string
	Signer       publisher.PayloadSigner
	UnsignedDev  bool
	PublishedAt  time.Time
	RollbackMode bool
}

type MoveResult struct {
	Manifest *manifest.Manifest `json:"manifest"`
	Action   string             `json:"action"`
}

type GCOptions struct {
	Backend storage.Backend
	DryRun  bool
}

type GCResult struct {
	Referenced int      `json:"referenced"`
	Deleted    []string `json:"deleted"`
	Kept       []string `json:"kept"`
}

type DoctorOptions struct {
	Backend         storage.Backend
	SigningKeyPath  string
	PublicKeyPath   string
	RequireObjects  bool
	RequireReleases bool
	RequireChannels bool
}

type DoctorResult struct {
	OK       bool     `json:"ok"`
	Warnings []string `json:"warnings"`
	Checks   []string `json:"checks"`
}

func Verify(ctx context.Context, opts VerifyOptions) (*VerifyResult, error) {
	if opts.Backend == nil {
		return nil, errors.New("backend is required")
	}
	data, err := loadManifestData(ctx, opts.Backend, opts.Version, opts.Channel)
	if err != nil {
		return nil, err
	}
	if opts.Verifier == nil && !opts.AllowUnsignedDev {
		return nil, errors.New("manifest verifier is required")
	}
	if opts.Verifier != nil {
		if err := opts.Verifier.VerifyManifest(ctx, data); err != nil {
			return nil, err
		}
	}

	m, err := decodeEnvelopeManifest(data)
	if err != nil {
		return nil, err
	}
	if err := verifyObjects(ctx, opts.Backend, m); err != nil {
		return nil, err
	}
	return &VerifyResult{Manifest: m, ObjectsChecked: len(m.Files)}, nil
}

func Promote(ctx context.Context, opts MoveOptions) (*MoveResult, error) {
	return moveChannel(ctx, opts, "promote")
}

func Rollback(ctx context.Context, opts MoveOptions) (*MoveResult, error) {
	opts.RollbackMode = true
	return moveChannel(ctx, opts, "rollback")
}

func GC(ctx context.Context, opts GCOptions) (*GCResult, error) {
	if opts.Backend == nil {
		return nil, errors.New("backend is required")
	}
	referenced, err := referencedObjects(ctx, opts.Backend)
	if err != nil {
		return nil, err
	}
	objects, err := opts.Backend.ListObjects(ctx)
	if err != nil {
		return nil, err
	}

	result := &GCResult{
		Referenced: len(referenced),
		Deleted:    []string{},
		Kept:       []string{},
	}
	for _, hash := range objects {
		if referenced[hash] {
			result.Kept = append(result.Kept, hash)
			continue
		}
		result.Deleted = append(result.Deleted, hash)
		if !opts.DryRun {
			if err := opts.Backend.DeleteObject(ctx, hash); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func Doctor(ctx context.Context, opts DoctorOptions) (*DoctorResult, error) {
	if opts.Backend == nil {
		return nil, errors.New("backend is required")
	}
	result := &DoctorResult{OK: true, Warnings: []string{}, Checks: []string{}}

	if opts.SigningKeyPath != "" {
		if _, err := os.Stat(opts.SigningKeyPath); err != nil {
			result.OK = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("signing key unavailable: %v", err))
		} else {
			result.Checks = append(result.Checks, "signing key exists")
		}
	}
	if opts.PublicKeyPath != "" {
		if _, err := os.Stat(opts.PublicKeyPath); err != nil {
			result.OK = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("public key unavailable: %v", err))
		} else {
			result.Checks = append(result.Checks, "public key exists")
		}
	}

	objects, err := opts.Backend.ListObjects(ctx)
	if err != nil {
		result.OK = false
		result.Warnings = append(result.Warnings, fmt.Sprintf("list objects failed: %v", err))
	} else {
		result.Checks = append(result.Checks, fmt.Sprintf("objects listed: %d", len(objects)))
		if opts.RequireObjects && len(objects) == 0 {
			result.OK = false
			result.Warnings = append(result.Warnings, "no objects found")
		}
	}
	versions, err := opts.Backend.ListReleaseVersions(ctx)
	if err != nil {
		result.OK = false
		result.Warnings = append(result.Warnings, fmt.Sprintf("list releases failed: %v", err))
	} else {
		result.Checks = append(result.Checks, fmt.Sprintf("release manifests listed: %d", len(versions)))
		if opts.RequireReleases && len(versions) == 0 {
			result.OK = false
			result.Warnings = append(result.Warnings, "no release manifests found")
		}
	}
	channels, err := opts.Backend.ListChannels(ctx)
	if err != nil {
		result.OK = false
		result.Warnings = append(result.Warnings, fmt.Sprintf("list channels failed: %v", err))
	} else {
		result.Checks = append(result.Checks, fmt.Sprintf("channel manifests listed: %d", len(channels)))
		if opts.RequireChannels && len(channels) == 0 {
			result.OK = false
			result.Warnings = append(result.Warnings, "no channel manifests found")
		}
	}
	return result, nil
}

func moveChannel(ctx context.Context, opts MoveOptions, action string) (*MoveResult, error) {
	if opts.Backend == nil {
		return nil, errors.New("backend is required")
	}
	if opts.Version == "" {
		return nil, errors.New("version is required")
	}
	if opts.Channel == "" {
		return nil, errors.New("channel is required")
	}
	if opts.Signer == nil && !opts.UnsignedDev {
		return nil, errors.New("manifest signer is required")
	}
	if opts.PublishedAt.IsZero() {
		opts.PublishedAt = time.Now().UTC()
	}

	data, err := opts.Backend.GetReleaseManifest(ctx, opts.Version)
	if err != nil {
		return nil, err
	}
	m, err := decodeEnvelopeManifest(data)
	if err != nil {
		return nil, err
	}
	if m.Version != opts.Version {
		return nil, fmt.Errorf("release manifest version = %q, want %q", m.Version, opts.Version)
	}
	if err := verifyObjects(ctx, opts.Backend, m); err != nil {
		return nil, err
	}

	next, err := nextReleaseSequence(ctx, opts.Backend, opts.Channel)
	if err != nil {
		return nil, err
	}
	m.Channel = opts.Channel
	m.ReleaseSequence = next
	m.PublishedAt = opts.PublishedAt.UTC()

	payload, err := manifest.EncodePayload(m)
	if err != nil {
		return nil, err
	}
	var sig *manifest.Signature
	if opts.Signer != nil {
		sig, err = opts.Signer.SignPayload(ctx, payload)
		if err != nil {
			return nil, err
		}
	}
	envelope, err := manifest.EncodeEnvelope(payload, sig)
	if err != nil {
		return nil, err
	}
	envelope = append(envelope, '\n')

	if err := opts.Backend.PutChannelManifest(ctx, opts.Channel, envelope); err != nil {
		return nil, err
	}
	return &MoveResult{Manifest: m, Action: action}, nil
}

func loadManifestData(ctx context.Context, backend storage.Backend, version string, channel string) ([]byte, error) {
	if version != "" && channel != "" {
		return nil, errors.New("choose either version or channel, not both")
	}
	if version != "" {
		return backend.GetReleaseManifest(ctx, version)
	}
	if channel != "" {
		return backend.GetChannelManifest(ctx, channel)
	}
	return nil, errors.New("version or channel is required")
}

func decodeEnvelopeManifest(data []byte) (*manifest.Manifest, error) {
	payload, _, err := manifest.DecodeEnvelope(data)
	if err != nil {
		return nil, err
	}
	return manifest.DecodeManifest(payload)
}

func verifyObjects(ctx context.Context, backend storage.Backend, m *manifest.Manifest) error {
	for _, file := range m.Files {
		reader, err := backend.GetObject(ctx, file.SHA256)
		if err != nil {
			return fmt.Errorf("get object for %s: %w", file.Path, err)
		}
		hash, hashErr := hashReader(reader)
		closeErr := reader.Close()
		if hashErr != nil {
			return hashErr
		}
		if closeErr != nil {
			return closeErr
		}
		if hash != file.SHA256 {
			return fmt.Errorf("object hash mismatch for %s", file.Path)
		}
	}
	return nil
}

func referencedObjects(ctx context.Context, backend storage.Backend) (map[string]bool, error) {
	referenced := map[string]bool{}

	versions, err := backend.ListReleaseVersions(ctx)
	if err != nil {
		return nil, err
	}
	for _, version := range versions {
		data, err := backend.GetReleaseManifest(ctx, version)
		if err != nil {
			return nil, err
		}
		m, err := decodeEnvelopeManifest(data)
		if err != nil {
			return nil, err
		}
		for _, file := range m.Files {
			referenced[file.SHA256] = true
		}
	}

	channels, err := backend.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		data, err := backend.GetChannelManifest(ctx, channel)
		if err != nil {
			return nil, err
		}
		m, err := decodeEnvelopeManifest(data)
		if err != nil {
			return nil, err
		}
		for _, file := range m.Files {
			referenced[file.SHA256] = true
		}
	}
	return referenced, nil
}

func nextReleaseSequence(ctx context.Context, backend storage.Backend, channel string) (int64, error) {
	data, err := backend.GetChannelManifest(ctx, channel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, nil
		}
		return 0, err
	}
	m, err := decodeEnvelopeManifest(data)
	if err != nil {
		return 0, err
	}
	return m.ReleaseSequence + 1, nil
}

func hashReader(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func SortStrings(values []string) []string {
	cp := append([]string(nil), values...)
	sort.Strings(cp)
	return cp
}
