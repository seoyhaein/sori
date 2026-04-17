package sori

import (
	"context"
	"net/http"
	"os"
	"time"
)

type Client struct {
	localStorePath string
	httpClient     *http.Client
	now            func() time.Time
}

type ClientOption func(*Client)

func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		localStorePath: defaultOCIStore,
		httpClient:     nil,
		now:            time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

func WithLocalStorePath(path string) ClientOption {
	return func(c *Client) {
		if path != "" {
			c.localStorePath = path
		}
	}
}

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

func WithClock(now func() time.Time) ClientOption {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

func (c *Client) LocalStorePath() string {
	return c.localStorePath
}

func (c *Client) PackageVolume(ctx context.Context, req PackageRequest) (*PackageResult, error) {
	return c.PackageVolumeWithOptions(ctx, req, PackageOptions{})
}

func (c *Client) PackageVolumeWithOptions(ctx context.Context, req PackageRequest, opts PackageOptions) (*PackageResult, error) {
	req.ConfigBlob = opts.ConfigBlob
	return packageVolumeToStoreWithOptions(ctx, c.localStorePath, req, opts)
}

func (c *Client) PushPackagedVolume(ctx context.Context, pkg *PackageResult, target RemoteTarget) (*PushResult, error) {
	return c.PushPackagedVolumeWithOptions(ctx, pkg, PushOptions{Target: target})
}

func (c *Client) PushPackagedVolumeWithOptions(ctx context.Context, pkg *PackageResult, opts PushOptions) (*PushResult, error) {
	target := opts.Target
	if c.httpClient != nil {
		target.HTTPClient = c.httpClient
	}
	return PushPackagedVolume(ctx, c.localStorePath, pkg, target)
}

func (c *Client) FetchVolumeSequential(ctx context.Context, destRoot, repo, tag string) (*VolumeIndex, error) {
	return c.FetchVolume(ctx, destRoot, repo, tag, FetchOptions{Concurrency: 1})
}

func (c *Client) FetchVolumeParallel(ctx context.Context, destRoot, repo, tag string, concurrency int) (*VolumeIndex, error) {
	return c.FetchVolume(ctx, destRoot, repo, tag, FetchOptions{Concurrency: concurrency})
}

func (c *Client) FetchVolume(ctx context.Context, destRoot, repo, tag string, opts FetchOptions) (*VolumeIndex, error) {
	if opts.RequireEmptyDestination {
		if err := ensureEmptyDir(destRoot); err != nil {
			return nil, err
		}
	}
	if opts.Concurrency <= 1 {
		return FetchVolSeq(ctx, destRoot, repo, tag)
	}
	return FetchVolParallel(ctx, destRoot, repo, tag, opts.Concurrency)
}

func ensureEmptyDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return transportError("FetchVolume", "read destination directory", err)
	}
	if len(entries) > 0 {
		return conflictError("FetchVolume", "destination directory is not empty", nil)
	}
	return nil
}

func (c *Client) PublishVolume(ctx context.Context, vi *VolumeIndex, volPath, volName string, configBlob []byte) (*VolumeIndex, error) {
	return vi.publishVolumeToStore(ctx, c.localStorePath, volPath, volName, configBlob)
}

func (c *Client) PublishVolumeFromDir(ctx context.Context, volDir, displayName, tag string) (*PackageResult, error) {
	return c.PackageVolume(ctx, PackageRequest{
		SourceDir:   volDir,
		DisplayName: displayName,
		Tag:         tag,
	})
}
