package storage

import (
	"context"
	"io"
)

type Backend interface {
	PutObject(ctx context.Context, sha256 string, data io.Reader) error
	ObjectExists(ctx context.Context, sha256 string) (bool, error)
	DeleteObject(ctx context.Context, sha256 string) error

	PutReleaseManifest(ctx context.Context, version string, data []byte) error
	GetReleaseManifest(ctx context.Context, version string) ([]byte, error)
	PutChannelManifest(ctx context.Context, channel string, data []byte) error
	GetChannelManifest(ctx context.Context, channel string) ([]byte, error)
	DeleteReleaseManifest(ctx context.Context, version string) error
}
