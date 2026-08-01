// Package github provides a client for fetching GitHub release information.
package github

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Release represents a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a release asset.
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

// defaultAPIBase is the GitHub REST API base used unless GITHUB_API_PREFIX
// overrides it (e.g. to point the integration test harness at a local mock).
const defaultAPIBase = "https://api.github.com"

// Client for GitHub API.
type Client struct {
	HTTPClient *http.Client
	// BaseURL is the API base. Empty means the real GitHub API.
	BaseURL string
}

// NewClient creates a new GitHub client. The API base defaults to the real
// GitHub REST API and can be overridden with GITHUB_API_PREFIX (e.g. to point
// the integration harness at a local mock); an empty/whitespace override
// falls back to the default.
func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		BaseURL:    os.Getenv("GITHUB_API_PREFIX"),
	}
}

// apiBase returns the effective API base with any trailing slash removed, so
// callers can join paths without producing double slashes.
func (c *Client) apiBase() string {
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		base = defaultAPIBase
	}
	return strings.TrimRight(base, "/")
}

// downloadTimeout bounds the whole asset download (connection, redirects and
// body). It is a package-level variable so tests can shorten it.
var downloadTimeout = 10 * time.Minute

// LatestRelease fetches the latest release for a repo.
func (c *Client) LatestRelease(owner, name string) (*Release, error) {
	apiURL := c.apiBase() + "/repos/" + owner + "/" + name + "/releases/latest"

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("crear request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("conexión: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rate-limit (HTTP 403): %s", string(body))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decodificar JSON: %w", err)
	}
	return &rel, nil
}

// FindAsset tries to find the best matching asset for a given base name.
// Strategies: exact match → prefix match → contains match.
func FindAsset(assets []Asset, baseName string) *Asset {
	// 1. Exact
	for i := range assets {
		if assets[i].Name == baseName {
			return &assets[i]
		}
	}
	// 2. Prefix
	for i := range assets {
		if strings.HasPrefix(assets[i].Name, baseName) {
			return &assets[i]
		}
	}
	// 3. Contains
	for i := range assets {
		if strings.Contains(assets[i].Name, baseName) {
			return &assets[i]
		}
	}
	return nil
}

// DownloadProgressReader wraps an io.Reader and calls a callback with bytes read.
type DownloadProgressReader struct {
	Reader    io.Reader
	Total     int64
	ReadSoFar int64
	Callback  func(bytesRead, total int64)
}

func (r *DownloadProgressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.ReadSoFar += int64(n)
	if r.Callback != nil && r.Total > 0 {
		r.Callback(r.ReadSoFar, r.Total)
	}
	return n, err
}

// DownloadAsset downloads an asset to a local file with progress reporting.
// The progressFn receives (bytesDownloaded, totalBytes). The download is
// bounded by downloadTimeout; when the response announces a Content-Length,
// the number of bytes written must match it exactly. Any failure removes the
// destination file.
func DownloadAsset(url, destPath string, progressFn func(downloaded, total int64)) error {
	client := &http.Client{Timeout: downloadTimeout}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("crear archivo: %w", err)
	}

	totalSize := resp.ContentLength

	progressReader := &DownloadProgressReader{
		Reader:   resp.Body,
		Total:    totalSize,
		Callback: progressFn,
	}

	written, err := io.Copy(out, progressReader)
	if err != nil {
		out.Close()
		os.Remove(destPath)
		return fmt.Errorf("escritura: %w", err)
	}

	// Verify the announced length when known (chunked/unknown-length bodies
	// are skipped); a mismatch means the transfer was truncated.
	if resp.ContentLength >= 0 && written != resp.ContentLength {
		out.Close()
		os.Remove(destPath)
		return fmt.Errorf("descarga incompleta: se esperaban %d bytes, se escribieron %d", resp.ContentLength, written)
	}

	if err := out.Close(); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("cerrar archivo: %w", err)
	}

	fmt.Printf("Descargados %d bytes a %s\n", written, destPath)
	return nil
}

// VerifyChecksum computes SHA256 of a file and compares it with an expected hex string.
func VerifyChecksum(filePath, expectedHex string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("abrir archivo: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("calcular SHA256: %w", err)
	}

	got := fmt.Sprintf("%x", h.Sum(nil))
	if !strings.EqualFold(got, expectedHex) {
		return fmt.Errorf("SHA256 mismatch: esperado %s, obtenido %s", expectedHex, got)
	}
	return nil
}

// DefaultAssetName builds the expected asset name following the pattern "appName-OS-ARCH".
func DefaultAssetName(appName, platformOS, platformArch string) string {
	if platformOS == "" {
		platformOS = detectOS()
	}
	if platformArch == "" {
		platformArch = detectArch()
	}
	return fmt.Sprintf("%s-%s-%s", appName, platformOS, platformArch)
}

func detectOS() string {
	// Use a reasonable default based on runtime
	return ""
}

func detectArch() string {
	return ""
}
