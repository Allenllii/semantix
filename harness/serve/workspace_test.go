package serve

import (
	"encoding/json"
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

// TestServeWorkspaceSelectorContract pins the GUI-2 (#405) selector wiring:
// hydration hooks in the page, the fragment-token auth bootstrap (same house
// contract as index), and the exact backend endpoints shell.js may call.
func TestServeWorkspaceSelectorContract(t *testing.T) {
	htmlWants := []string{
		`data-ws-project`, `data-ws-project-name`,
		`data-ws-branch`, `data-ws-branch-menu`,
		`data-ws-model`, `data-ws-model-menu`,
		`data-ws-effort`, `data-ws-effort-menu`,
		`data-ws-notice`,
		// GUI-3 (#406) sidebar
		`data-ws-new-task`, `data-ws-task-list`, `data-ws-side-project-name`,
	}
	for _, want := range htmlWants {
		if !strings.Contains(string(workspaceHTML), want) {
			t.Errorf("workspace page missing selector hook %q", want)
		}
	}

	js := string(workspaceShellJS)
	for _, want := range []string{
		`URLSearchParams(window.location.hash.slice(1))`, // fragment-token bootstrap,
		`/auth/token`, // same house contract as index.html
		`window.history.replaceState`,
		`"/status"`,   // project name from real backend state
		`"/branches"`, // branch display from real backend state
		`"/models"`,   // model list + current + effort
		`"/submit"`,   // switches reuse the CLI command surface
		`"/model "`,   //   .../model <ref>
		`"/effort "`,  //   .../effort <level>
		`"/sessions"`, // task list = live sessions, no second data model (#406)
		`"/resume"`,   // task switching keeps session content server-side
		`"/new"`,      // creating a task enters a fresh session
		`"/workspace/events"`, // GUI-4 versioned SSE transport
		`模型不可用`,       // explicit unavailable-model signal (#405 acceptance)
	} {
		if !strings.Contains(js, want) {
			t.Errorf("workspace shell.js missing %q", want)
		}
	}

	// Guard rails: the shell talks only to the whitelisted endpoints above —
	// it must not use the legacy raw stream or invent a second history model.
	for _, forbidden := range []string{`"/events"`, `/history`} {
		if strings.Contains(js, forbidden) {
			t.Errorf("workspace shell.js dials out-of-contract endpoint %s", forbidden)
		}
	}
}

// TestServeSessionsExposeInFlight verifies the /sessions payload the sidebar
// consumes: entries keep name/path identity and can flag running sessions via
// the existing branch sidecar marker — still one shared data model (#406).
func TestServeSessionsExposeInFlight(t *testing.T) {
	srv := newWorkspaceTestServer(t)

	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sessions status = %d", resp.StatusCode)
	}

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode /sessions: %v", err)
	}
	for i, row := range rows {
		if _, ok := row["name"].(string); !ok {
			t.Errorf("/sessions[%d] missing string name", i)
		}
		if _, ok := row["path"].(string); !ok {
			t.Errorf("/sessions[%d] missing string path", i)
		}
		for key, want := range map[string]string{"in_flight": "bool", "current": "bool", "turns": "number", "title": "string"} {
			v, ok := row[key]
			if !ok {
				continue
			}
			switch want {
			case "bool":
				if _, ok := v.(bool); !ok {
					t.Errorf("/sessions[%d].%s = %#v, want bool", i, key, v)
				}
			case "number":
				if _, ok := v.(float64); !ok {
					t.Errorf("/sessions[%d].%s = %#v, want number", i, key, v)
				}
			case "string":
				if _, ok := v.(string); !ok {
					t.Errorf("/sessions[%d].%s = %#v, want string", i, key, v)
				}
			}
		}
	}
}

// TestServeModelsExposesEffort ensures GET /models carries the active
// provider's reasoning effort so the effort chip reflects real config state.
func TestServeModelsExposesEffort(t *testing.T) {
	srv := newWorkspaceTestServer(t)

	resp, err := http.Get(srv.URL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /models status = %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"current", "label", "default", "effort", "models"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("GET /models response missing %q", key)
		}
	}
	if _, ok := payload["effort"].(string); !ok {
		t.Errorf("GET /models effort = %#v, want string", payload["effort"])
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
