package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethan-mdev/patchline/pkg/manifest"
)

type Backend struct {
	root string
}

func New(root string) *Backend {
	return &Backend{root: filepath.Clean(root)}
}

func (b *Backend) Root() string {
	return b.root
}

func (b *Backend) PutObject(ctx context.Context, sha256 string, data io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := manifest.ObjectKeyForHash(sha256)
	if err != nil {
		return err
	}
	path := filepath.Join(b.root, filepath.FromSlash(key))
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeFileAtomic(path, data)
}

func (b *Backend) ObjectExists(ctx context.Context, sha256 string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	key, err := manifest.ObjectKeyForHash(sha256)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(b.root, filepath.FromSlash(key)))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (b *Backend) DeleteObject(ctx context.Context, sha256 string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := manifest.ObjectKeyForHash(sha256)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(b.root, filepath.FromSlash(key)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (b *Backend) PutReleaseManifest(ctx context.Context, version string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateKeyPart("version", version); err != nil {
		return err
	}
	path := filepath.Join(b.root, "releases", version, "manifest.json")
	return writeFileAtomic(path, bytesReader(data))
}

func (b *Backend) GetReleaseManifest(ctx context.Context, version string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateKeyPart("version", version); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(b.root, "releases", version, "manifest.json"))
}

func (b *Backend) PutChannelManifest(ctx context.Context, channel string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateKeyPart("channel", channel); err != nil {
		return err
	}
	path := filepath.Join(b.root, "channels", channel, "manifest.json")
	return writeFileAtomic(path, bytesReader(data))
}

func (b *Backend) GetChannelManifest(ctx context.Context, channel string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateKeyPart("channel", channel); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(b.root, "channels", channel, "manifest.json"))
}

func (b *Backend) DeleteReleaseManifest(ctx context.Context, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateKeyPart("version", version); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(b.root, "releases", version, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func writeFileAtomic(path string, data io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".patchline-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()

	_, copyErr := io.Copy(tempFile, data)
	closeErr := tempFile.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func validateKeyPart(name string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("%s is invalid", name)
	}
	if strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s must not contain path separators", name)
	}
	return nil
}

func bytesReader(data []byte) io.Reader {
	return &byteSliceReader{data: data}
}

type byteSliceReader struct {
	data []byte
	off  int
}

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}
