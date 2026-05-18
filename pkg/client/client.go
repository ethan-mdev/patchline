package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethan-mdev/patchline/pkg/manifest"
	"github.com/ethan-mdev/patchline/pkg/patch"
)

type Config struct {
	AppID               string
	Channel             string
	BaseURL             string
	InstallDir          string
	LastReleaseSequence int64
	HTTPClient          *http.Client
}

type Client struct {
	appID               string
	channel             string
	baseURL             string
	installDir          string
	lastReleaseSequence int64
	httpClient          *http.Client
}

type Plan struct {
	Manifest   *manifest.Manifest `json:"manifest"`
	Files      []FileUpdate       `json:"files"`
	TotalBytes int64              `json:"total_bytes"`
}

type FileUpdate struct {
	File   manifest.File `json:"file"`
	Reason string        `json:"reason"`
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, errors.New("app_id is required")
	}
	if strings.TrimSpace(cfg.Channel) == "" {
		return nil, errors.New("channel is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("base_url is required")
	}
	parsed, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid base_url %q", cfg.BaseURL)
	}
	if strings.TrimSpace(cfg.InstallDir) == "" {
		return nil, errors.New("install_dir is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		appID:               cfg.AppID,
		channel:             cfg.Channel,
		baseURL:             strings.TrimRight(cfg.BaseURL, "/"),
		installDir:          filepath.Clean(cfg.InstallDir),
		lastReleaseSequence: cfg.LastReleaseSequence,
		httpClient:          httpClient,
	}, nil
}

func (c *Client) FetchChannelManifest(ctx context.Context) (*manifest.Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/channels/"+url.PathEscape(c.channel)+"/manifest.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch channel manifest: status %d", resp.StatusCode)
	}

	var m manifest.Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode channel manifest: %w", err)
	}
	if err := c.validateManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *Client) Plan(ctx context.Context, m *manifest.Manifest) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.validateManifest(m); err != nil {
		return nil, err
	}

	plan := &Plan{Manifest: m, Files: []FileUpdate{}}
	for _, file := range m.Files {
		localPath, err := c.localPath(file.Path)
		if err != nil {
			return nil, err
		}

		localHash, err := patch.HashFile(localPath)
		if err != nil {
			if os.IsNotExist(err) {
				plan.Files = append(plan.Files, FileUpdate{File: file, Reason: "missing"})
				plan.TotalBytes += file.Size
				continue
			}
			return nil, err
		}
		if localHash != file.SHA256 {
			plan.Files = append(plan.Files, FileUpdate{File: file, Reason: "changed"})
			plan.TotalBytes += file.Size
		}
	}
	return plan, nil
}

func (c *Client) Apply(ctx context.Context, plan *Plan) error {
	if plan == nil {
		return errors.New("plan is nil")
	}
	if err := c.validateManifest(plan.Manifest); err != nil {
		return err
	}

	for _, update := range plan.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.applyFile(ctx, update.File); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) validateManifest(m *manifest.Manifest) error {
	if err := manifest.Validate(m); err != nil {
		return err
	}
	if m.AppID != c.appID {
		return fmt.Errorf("manifest app_id = %q, want %q", m.AppID, c.appID)
	}
	if m.Channel != c.channel {
		return fmt.Errorf("manifest channel = %q, want %q", m.Channel, c.channel)
	}
	if c.lastReleaseSequence > 0 && m.ReleaseSequence <= c.lastReleaseSequence {
		return fmt.Errorf("manifest release_sequence %d is not newer than %d", m.ReleaseSequence, c.lastReleaseSequence)
	}
	return nil
}

func (c *Client) applyFile(ctx context.Context, file manifest.File) error {
	localPath, err := c.localPath(file.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(localPath), ".patchline-download-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()

	copyErr := c.downloadObject(ctx, file, tempFile)
	closeErr := tempFile.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}

	hash, err := patch.HashFile(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if hash != file.SHA256 {
		_ = os.Remove(tempPath)
		return fmt.Errorf("hash mismatch for %s", file.Path)
	}

	if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, localPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func (c *Client) downloadObject(ctx context.Context, file manifest.File, dst io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.objectURL(file.ObjectKey), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", file.Path, resp.StatusCode)
	}
	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		return err
	}
	if written != file.Size {
		return fmt.Errorf("download %s: wrote %d bytes, want %d", file.Path, written, file.Size)
	}
	return nil
}

func (c *Client) objectURL(objectKey string) string {
	segments := strings.Split(objectKey, "/")
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return c.baseURL + "/" + strings.Join(escaped, "/")
}

func (c *Client) localPath(manifestPath string) (string, error) {
	cleanPath, err := patch.CleanRelativePath(manifestPath)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(c.installDir, filepath.FromSlash(cleanPath))
	rel, err := filepath.Rel(c.installDir, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid install path %q", manifestPath)
	}
	return fullPath, nil
}
