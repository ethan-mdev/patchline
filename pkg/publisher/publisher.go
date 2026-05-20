package publisher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethan-mdev/patchline/pkg/manifest"
	"github.com/ethan-mdev/patchline/pkg/patch"
	"github.com/ethan-mdev/patchline/pkg/storage"
)

type Options struct {
	AppID           string
	Version         string
	Channel         string
	PublishedAt     time.Time
	ReleaseSequence int64
	Signer          PayloadSigner
	UnsignedDev     bool
}

type Result struct {
	Manifest        *manifest.Manifest   `json:"manifest"`
	Signature       *manifest.Signature  `json:"signature,omitempty"`
	ObjectsUploaded int                  `json:"objects_uploaded"`
	ObjectsReused   int                  `json:"objects_reused"`
}

// PayloadSigner produces a detached signature over the canonical payload bytes
// of a manifest. Implementations must not modify the input bytes.
type PayloadSigner interface {
	SignPayload(ctx context.Context, payload []byte) (*manifest.Signature, error)
}

func Publish(ctx context.Context, backend storage.Backend, buildDir string, opts Options) (*Result, error) {
	if backend == nil {
		return nil, errors.New("backend is required")
	}
	if opts.PublishedAt.IsZero() {
		opts.PublishedAt = time.Now().UTC()
	}
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if opts.ReleaseSequence == 0 {
		sequence, err := nextReleaseSequence(ctx, backend, opts.Channel)
		if err != nil {
			return nil, err
		}
		opts.ReleaseSequence = sequence
	}

	scanned, err := patch.ScanDir(ctx, buildDir, patch.ScanOptions{
		ExcludeNames: map[string]bool{
			"manifest.json": true,
		},
	})
	if err != nil {
		return nil, err
	}

	files := make([]manifest.File, 0, len(scanned))
	result := &Result{}
	for _, file := range scanned {
		objectKey, err := manifest.ObjectKeyForHash(file.SHA256)
		if err != nil {
			return nil, err
		}
		files = append(files, manifest.File{
			Path:      file.Path,
			Size:      file.Size,
			SHA256:    file.SHA256,
			ObjectKey: objectKey,
		})

		exists, err := backend.ObjectExists(ctx, file.SHA256)
		if err != nil {
			return nil, err
		}
		if exists {
			result.ObjectsReused++
			continue
		}
		source, err := os.Open(filepath.Join(buildDir, filepath.FromSlash(file.Path)))
		if err != nil {
			return nil, err
		}
		putErr := backend.PutObject(ctx, file.SHA256, source)
		closeErr := source.Close()
		if putErr != nil {
			return nil, putErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		result.ObjectsUploaded++
	}

	m := &manifest.Manifest{
		FormatVersion:   manifest.FormatVersion,
		AppID:           opts.AppID,
		Version:         opts.Version,
		Channel:         opts.Channel,
		ReleaseSequence: opts.ReleaseSequence,
		PublishedAt:     opts.PublishedAt.UTC(),
		Files:           files,
	}
	payload, err := manifest.EncodePayload(m)
	if err != nil {
		return nil, err
	}
	if opts.Signer == nil && !opts.UnsignedDev {
		return nil, errors.New("manifest signer is required")
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

	if err := backend.PutReleaseManifest(ctx, m.Version, envelope); err != nil {
		return nil, err
	}
	if err := verifyObjects(ctx, backend, m); err != nil {
		return nil, err
	}
	if err := backend.PutChannelManifest(ctx, m.Channel, envelope); err != nil {
		return nil, err
	}

	result.Manifest = m
	result.Signature = sig
	return result, nil
}

func validateOptions(opts Options) error {
	m := &manifest.Manifest{
		FormatVersion:   manifest.FormatVersion,
		AppID:           opts.AppID,
		Version:         opts.Version,
		Channel:         opts.Channel,
		ReleaseSequence: 1,
		PublishedAt:     opts.PublishedAt,
		Files:           []manifest.File{},
	}
	return manifest.Validate(m)
}

func nextReleaseSequence(ctx context.Context, backend storage.Backend, channel string) (int64, error) {
	data, err := backend.GetChannelManifest(ctx, channel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, nil
		}
		return 0, err
	}
	payload, _, err := manifest.DecodeEnvelope(data)
	if err != nil {
		return 0, fmt.Errorf("decode channel envelope: %w", err)
	}
	current, err := manifest.DecodeManifest(payload)
	if err != nil {
		return 0, fmt.Errorf("decode channel manifest: %w", err)
	}
	return current.ReleaseSequence + 1, nil
}

func verifyObjects(ctx context.Context, backend storage.Backend, m *manifest.Manifest) error {
	for _, file := range m.Files {
		exists, err := backend.ObjectExists(ctx, file.SHA256)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("published object missing for %s", file.Path)
		}
	}
	return nil
}
