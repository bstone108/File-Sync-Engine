package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultGitHubAPIBase   = "https://api.github.com"
	defaultGitHubOwner     = "bstone108"
	defaultGitHubRepo      = "File-Sync-Engine"
	desktopUpdateUserAgent = "File-Sync-Engine-Desktop"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

type githubReleaseClient interface {
	LatestRelease(ctx context.Context) (githubRelease, error)
	Download(ctx context.Context, url string) (io.ReadCloser, error)
}

type httpGitHubReleaseClient struct {
	baseURL    string
	owner      string
	repo       string
	httpClient *http.Client
}

func newHTTPGitHubReleaseClient() *httpGitHubReleaseClient {
	return &httpGitHubReleaseClient{
		baseURL: defaultGitHubAPIBase,
		owner:   defaultGitHubOwner,
		repo:    defaultGitHubRepo,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *httpGitHubReleaseClient) LatestRelease(ctx context.Context) (githubRelease, error) {
	url := strings.TrimRight(c.baseURL, "/") + "/repos/" + c.owner + "/" + c.repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", desktopUpdateUserAgent)
	resp, err := c.http().Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return githubRelease{}, fmt.Errorf("read GitHub latest release: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub latest release: HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub latest release: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, fmt.Errorf("GitHub latest release is missing a tag")
	}
	return release, nil
}

func (c *httpGitHubReleaseClient) Download(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", desktopUpdateUserAgent)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	return resp.Body, nil
}

func (c *httpGitHubReleaseClient) http() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
}

func (r githubRelease) Version() string {
	return strings.TrimPrefix(strings.TrimSpace(r.TagName), "v")
}

func (a githubAsset) SHA256() string {
	digest := strings.TrimSpace(a.Digest)
	if strings.HasPrefix(strings.ToLower(digest), "sha256:") {
		return strings.ToLower(strings.TrimSpace(digest[7:]))
	}
	return ""
}
