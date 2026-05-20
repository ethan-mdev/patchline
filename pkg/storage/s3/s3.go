package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/ethan-mdev/patchline/pkg/manifest"
)

type Config struct {
	Bucket         string
	Region         string
	Endpoint       string
	Prefix         string
	ForcePathStyle bool
}

type Backend struct {
	client *awss3.Client
	bucket string
	prefix string
}

func New(ctx context.Context, cfg Config) (*Backend, error) {
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	if cfg.Bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = "us-east-1"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}
	client := awss3.NewFromConfig(awsCfg, func(options *awss3.Options) {
		options.UsePathStyle = cfg.ForcePathStyle
		if strings.TrimSpace(cfg.Endpoint) != "" {
			options.BaseEndpoint = aws.String(strings.TrimSpace(cfg.Endpoint))
		}
	})
	return &Backend{
		client: client,
		bucket: cfg.Bucket,
		prefix: cleanPrefix(cfg.Prefix),
	}, nil
}

func (b *Backend) PutObject(ctx context.Context, sha256 string, data io.Reader) error {
	exists, err := b.ObjectExists(ctx, sha256)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	key, err := manifest.ObjectKeyForHash(sha256)
	if err != nil {
		return err
	}
	return b.put(ctx, key, data, "application/octet-stream", "public, max-age=31536000, immutable")
}

func (b *Backend) GetObject(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	key, err := manifest.ObjectKeyForHash(sha256)
	if err != nil {
		return nil, err
	}
	return b.getBody(ctx, key)
}

func (b *Backend) ObjectExists(ctx context.Context, sha256 string) (bool, error) {
	key, err := manifest.ObjectKeyForHash(sha256)
	if err != nil {
		return false, err
	}
	_, err = b.client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(key)),
	})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, err
}

func (b *Backend) DeleteObject(ctx context.Context, sha256 string) error {
	key, err := manifest.ObjectKeyForHash(sha256)
	if err != nil {
		return err
	}
	return b.delete(ctx, key)
}

func (b *Backend) ListObjects(ctx context.Context) ([]string, error) {
	keys, err := b.list(ctx, "objects/sha256/")
	if err != nil {
		return nil, err
	}
	hashes := make([]string, 0, len(keys))
	for _, key := range keys {
		name := path.Base(key)
		if manifest.IsSHA256(name) {
			hashes = append(hashes, name)
		}
	}
	sort.Strings(hashes)
	return hashes, nil
}

func (b *Backend) PutReleaseManifest(ctx context.Context, version string, data []byte) error {
	if err := validateKeyPart("version", version); err != nil {
		return err
	}
	return b.put(ctx, path.Join("releases", version, "manifest.json"), bytes.NewReader(data), "application/json", "no-cache")
}

func (b *Backend) GetReleaseManifest(ctx context.Context, version string) ([]byte, error) {
	if err := validateKeyPart("version", version); err != nil {
		return nil, err
	}
	return b.get(ctx, path.Join("releases", version, "manifest.json"))
}

func (b *Backend) ListReleaseVersions(ctx context.Context) ([]string, error) {
	keys, err := b.list(ctx, "releases/")
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0)
	for _, key := range keys {
		if !strings.HasSuffix(key, "/manifest.json") {
			continue
		}
		parts := strings.Split(key, "/")
		if len(parts) == 3 && parts[0] == "releases" && parts[2] == "manifest.json" {
			versions = append(versions, parts[1])
		}
	}
	sort.Strings(versions)
	return versions, nil
}

func (b *Backend) PutChannelManifest(ctx context.Context, channel string, data []byte) error {
	if err := validateKeyPart("channel", channel); err != nil {
		return err
	}
	return b.put(ctx, path.Join("channels", channel, "manifest.json"), bytes.NewReader(data), "application/json", "no-cache")
}

func (b *Backend) GetChannelManifest(ctx context.Context, channel string) ([]byte, error) {
	if err := validateKeyPart("channel", channel); err != nil {
		return nil, err
	}
	return b.get(ctx, path.Join("channels", channel, "manifest.json"))
}

func (b *Backend) ListChannels(ctx context.Context) ([]string, error) {
	keys, err := b.list(ctx, "channels/")
	if err != nil {
		return nil, err
	}
	channels := make([]string, 0)
	for _, key := range keys {
		if !strings.HasSuffix(key, "/manifest.json") {
			continue
		}
		parts := strings.Split(key, "/")
		if len(parts) == 3 && parts[0] == "channels" && parts[2] == "manifest.json" {
			channels = append(channels, parts[1])
		}
	}
	sort.Strings(channels)
	return channels, nil
}

func (b *Backend) DeleteReleaseManifest(ctx context.Context, version string) error {
	if err := validateKeyPart("version", version); err != nil {
		return err
	}
	return b.delete(ctx, path.Join("releases", version, "manifest.json"))
}

func (b *Backend) put(ctx context.Context, key string, data io.Reader, contentType string, cacheControl string) error {
	_, err := b.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:       aws.String(b.bucket),
		Key:          aws.String(b.key(key)),
		Body:         data,
		ContentType:  aws.String(contentType),
		CacheControl: aws.String(cacheControl),
	})
	return err
}

func (b *Backend) get(ctx context.Context, key string) ([]byte, error) {
	body, err := b.getBody(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}

func (b *Backend) getBody(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := b.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(key)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return out.Body, nil
}

func (b *Backend) delete(ctx context.Context, key string) error {
	_, err := b.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.key(key)),
	})
	return err
}

func (b *Backend) list(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := b.key(prefix)
	paginator := awss3.NewListObjectsV2Paginator(b.client, &awss3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(fullPrefix),
	})

	keys := make([]string, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			if object.Key == nil {
				continue
			}
			key := strings.TrimPrefix(*object.Key, b.prefix)
			key = strings.TrimPrefix(key, "/")
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (b *Backend) key(key string) string {
	key = strings.TrimPrefix(path.Clean(strings.TrimSpace(key)), "/")
	if b.prefix == "" {
		return key
	}
	return b.prefix + "/" + key
}

func cleanPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.Trim(prefix, "/")
	if prefix == "" || prefix == "." {
		return ""
	}
	return path.Clean(prefix)
}

func validateKeyPart(name string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("%s is invalid", name)
	}
	if strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return fmt.Errorf("%s must not contain path separators", name)
	}
	return nil
}

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "NoSuchKey", "NotFound", "404":
		return true
	default:
		return false
	}
}
