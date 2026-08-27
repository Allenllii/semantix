package serve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"semantix/harness/config"
	"semantix/harness/control"
)

// newWorkspaceTestServer builds a bare server like TestServeIndexPage does;
// the workspace shell renders without any controller interaction.
func newWorkspaceTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestServeWorkspaceShellRenders(t *testing.T) {
	srv := newWorkspaceTestServer(t)

	resp, err := http.Get(srv.URL + "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /workspace status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /workspace content-type = %q, want text/html", ct)
	}

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	for _, want := range []string{
		`data-ws-shell`,
		`data-ws-brand`,             // shared Semantix wordmark
		`/assets/logo-wordmark.svg`, // shared asset is the single branding source
		`id="ws-side"`,              // left nav rail
		`data-ws-side-toggle`,       // mobile drawer trigger
		`data-ws-side-close`,        // mobile drawer scrim
		`id="ws-context"`,           // right context panel
		`data-ws-collapse`,          // collapse control
		`aria-expanded="true"`,      // expanded by default at desktop widths
		`提出后续修改要求`,                  // composer placeholder
		`实现高缓存命中率`,                  // demo task title from the GUI-1 mockup
		`src/cache/prefix_cache.go`, // file tree + diff headers
	} {
		if !strings.Contains(html, want) {
			t.Errorf("workspace shell missing %q", want)
		}
	}
}

func TestServeWorkspaceUsesSemantixWordmark(t *testing.T) {
	logo := string(logoWordmarkSVG)
	if !strings.Contains(logo, `aria-label="Semantix"`) {
		t.Fatal("shared logo must identify Semantix")
	}
	if !strings.Contains(strings.ToLower(logo), "semantix") {
		t.Fatal("shared logo must contain the Semantix wordmark")
	}
	if strings.Contains(logo, "#0153e5") {
		t.Fatal("shared logo still contains the pre-rebrand blue Reasonix asset")
	}
}

func TestServeWorkspaceSideDrawerContract(t *testing.T) {

	for asset, wants := range map[string][]string{
		string(workspaceShellJS):   {"ws-side-open", "data-ws-side-toggle", "Escape", "aria-hidden"},
		string(workspaceLayoutCSS): {"@media (max-width: 860px)", "body.ws-side-open .ws-side", "ws-side-scrim"},
	} {
		for _, want := range wants {
			if !strings.Contains(asset, want) {
				t.Errorf("workspace drawer asset missing %q", want)
			}
		}
	}
}

// TestServeWorkspaceShellStandalone pins the issue's "renders without business
// data" acceptance rule: the page must not dial any backend endpoint.
func TestServeWorkspaceShellStandalone(t *testing.T) {
	for _, endpoint := range []string{`'/events'`, `"/events"`, `/submit`, `/history`} {
		if strings.Contains(string(workspaceHTML), endpoint) {
			t.Errorf("workspace shell dials backend endpoint %s", endpoint)
		}
	}
}

func TestServeWorkspaceAssets(t *testing.T) {
	srv := newWorkspaceTestServer(t)

	for path, wantCT := range map[string]string{
		"/workspace/tokens.css": "text/css",
		"/workspace/layout.css": "text/css",
		"/workspace/shell.js":   "text/javascript",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d", path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, wantCT) {
			t.Errorf("GET %s content-type = %q, want prefix %q", path, ct, wantCT)
		}
	}
}

// TestServeWorkspaceDesignTokens keeps the brand tokens aligned with their
// sources of truth (site palette, mockup semantics).
func TestServeWorkspaceDesignTokens(t *testing.T) {
	var needs = map[string][]string{
		string(workspaceTokensCSS): {"--sx-green: oklch(0.608 0.14 165)", "--sx-orange:", "--sx-bg-canvas:"},
		string(workspaceLayoutCSS): {`@media (max-width: 1180px)`, `@media (max-width: 860px)`},
	}
	for asset, wants := range needs {
		for _, want := range wants {
			if !strings.Contains(asset, want) {
				t.Errorf("workspace asset missing %q", want)
			}
		}
	}
}
