package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDownloadAssetSuccess verifies a well-formed download hits the API asset
// endpoint with Accept: application/octet-stream and the Bearer token, and
// writes the destination file with the exact advertised bytes.
func TestDownloadAssetSuccess(t *testing.T) {
	body := []byte("hello asset")
	var gotPath, gotAccept, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		w.Write(body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.tmp")
	c := &Client{BaseURL: srv.URL, Token: "tok-123"}
	if err := c.DownloadAsset("owner", "name", Asset{ID: 42}, dest, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(body) {
		t.Errorf("downloaded %q, want %q", data, body)
	}
	if want := "/repos/owner/name/releases/assets/42"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
	if want := "application/octet-stream"; gotAccept != want {
		t.Errorf("Accept header = %q, want %q", gotAccept, want)
	}
	if want := "Bearer tok-123"; gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

// TestDownloadAssetLengthMismatch verifies a body shorter than the advertised
// Content-Length fails and removes the destination file.
func TestDownloadAssetLengthMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("short"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.tmp")
	c := &Client{BaseURL: srv.URL}
	err := c.DownloadAsset("owner", "name", Asset{ID: 42}, dest, nil)
	if err == nil {
		t.Fatal("expected an error for a truncated body")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("destination should be removed, stat err = %v", statErr)
	}
}

// TestDownloadAssetTimeout verifies a stalled response fails within the
// (shortened) download timeout and removes the destination file.
func TestDownloadAssetTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done() // wait for the client to give up
	}))
	defer srv.Close()

	old := downloadTimeout
	downloadTimeout = 200 * time.Millisecond
	t.Cleanup(func() { downloadTimeout = old })

	dest := filepath.Join(t.TempDir(), "out.tmp")
	c := &Client{BaseURL: srv.URL}
	err := c.DownloadAsset("owner", "name", Asset{ID: 42}, dest, nil)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("destination should be removed, stat err = %v", statErr)
	}
}

// TestClientAPIBase covers the GITHUB_API_PREFIX override: empty/whitespace
// values fall back to the real GitHub API; non-empty values (with or without
// a trailing slash) are used as-is after trimming.
func TestClientAPIBase(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{"default", "", "https://api.github.com"},
		{"whitespace override", "   ", "https://api.github.com"},
		{"local mock", "http://localhost:9999", "http://localhost:9999"},
		{"trailing slash", "http://localhost:9999/", "http://localhost:9999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_API_PREFIX", tt.prefix)
			if got := NewClient().apiBase(); got != tt.want {
				t.Errorf("apiBase() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLatestReleaseAgainstMock verifies that LatestRelease honors the
// GITHUB_API_PREFIX override and reaches the mock's
// /repos/{owner}/{name}/releases/latest route (no double/missing slashes)
// with the standard Accept header.
func TestLatestReleaseAgainstMock(t *testing.T) {
	var gotPath string
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"tag_name":"v1.2.3","assets":[{"name":"a-linux-amd64","browser_download_url":"http://localhost:9999/dl/a"}]}`)
	}))
	defer srv.Close()

	t.Setenv("GITHUB_API_PREFIX", srv.URL+"/") // trailing slash must not double
	c := NewClient()
	c.HTTPClient = srv.Client() // deterministic transport, no proxy

	rel, err := c.LatestRelease("owner", "name")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("TagName = %q, want v1.2.3", rel.TagName)
	}
	if want := "/repos/owner/name/releases/latest"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
	if want := "application/vnd.github.v3+json"; gotAccept != want {
		t.Errorf("Accept header = %q, want %q", gotAccept, want)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Name != "a-linux-amd64" {
		t.Errorf("unexpected assets: %+v", rel.Assets)
	}
	// The asset download URL is passed through untouched (the mock supplies
	// an absolute URL; production GitHub assets must keep working).
	if rel.Assets[0].DownloadURL != "http://localhost:9999/dl/a" {
		t.Errorf("DownloadURL = %q, want the absolute mock URL", rel.Assets[0].DownloadURL)
	}
}

// TestNewClientToken covers the GITHUB_TOKEN env var: its value is trimmed
// into Client.Token.
func TestNewClientToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "  ghp_abc  ")
	if got := NewClient().Token; got != "ghp_abc" {
		t.Errorf("Token = %q, want %q", got, "ghp_abc")
	}
	t.Setenv("GITHUB_TOKEN", "   ")
	if got := NewClient().Token; got != "" {
		t.Errorf("Token = %q, want empty for whitespace-only env", got)
	}
}

// TestLatestReleaseAuth verifies the Bearer token is sent when configured and
// absent on anonymous requests.
func TestLatestReleaseAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"tag_name":"v1"}`)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL, Token: "tok-123"}
	if _, err := c.LatestRelease("o", "n"); err != nil {
		t.Fatal(err)
	}
	if want := "Bearer tok-123"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}

	c.Token = ""
	if _, err := c.LatestRelease("o", "n"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("anonymous request sent Authorization %q", gotAuth)
	}
}

// TestLatestReleasePrivateHint verifies a 404 without a token hints at
// GITHUB_TOKEN (GitHub hides private repos behind 404), and that the hint is
// absent once a token is configured.
func TestLatestReleasePrivateHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	_, err := c.LatestRelease("o", "private")
	if err == nil {
		t.Fatal("expected an error for a 404 response")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("anonymous 404 error lacks the GITHUB_TOKEN hint: %v", err)
	}

	c.Token = "tok-123"
	_, err = c.LatestRelease("o", "private")
	if err == nil {
		t.Fatal("expected an error for a 404 response")
	}
	if strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("authenticated 404 error should not suggest GITHUB_TOKEN: %v", err)
	}
}
