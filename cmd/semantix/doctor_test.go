package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// clearDoctorEnv neutralizes embedder/judge/config env vars so a developer
// machine's real settings cannot leak into doctor tests.
func clearDoctorEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"SEMANTIX_CONFIG", "SEMANTIX_DB",
		"SEMANTIX_EMBED_BASE_URL", "SEMANTIX_EMBED_MODEL", "SEMANTIX_EMBED_API_KEY",
		"SEMANTIX_JUDGE_API_KEY",
	} {
		t.Setenv(k, "")
	}
}

// chdirTemp runs the test with an empty working directory, so the default
// config path ./semantix.toml is guaranteed absent.
func chdirTemp(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sliceJSON renders one store line exactly as kernel/slice's FileStore
// writes it (json.Marshal: []byte content becomes base64), so the JSONL
// fixture is byte-for-byte faithful to a real store.
func sliceJSON(t *testing.T, id string) string {
	t.Helper()
	b, err := json.Marshal(slice.Slice{
		ID: id, Type: slice.Prompt, Scope: slice.Project, Content: []byte("content " + id),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestDoctorAllGreen: valid config + populated store + embedder/judge wired
// -> every check PASS, exit 0.
func TestDoctorAllGreen(t *testing.T) {
	clearDoctorEnv(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "project.db")
	writeJSONL(t, dbPath, sliceJSON(t, "a"), sliceJSON(t, "b"))
	cfgPath := writeConfig(t, fmt.Sprintf(`
[store]
db = %q
scope = "project"

[embed]
base_url = "https://api.openai.com/v1"
model = "text-embedding-3-small"

[judge]
base_url = "https://api.openai.com/v1"
model = "gpt-4o-mini"
protocol = "openai"
`, dbPath))
	t.Setenv("SEMANTIX_EMBED_API_KEY", "sk-test")
	t.Setenv("SEMANTIX_JUDGE_API_KEY", "sk-test")

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--config", cfgPath, "--db", dbPath}, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("doctor: code = %d, want 0; stdout = %q", code, stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"[PASS] config", "[PASS] slice store", "[PASS] embedder", "[PASS] judge",
		"checks: pass=4 warn=0 fail=0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "2 slices") {
		t.Errorf("stdout missing slice count:\n%s", out)
	}
}

// TestDoctorWarnsOnMissingStoreAndConfig: empty workspace -> config and
// store are WARN, embedder/judge are PASS/WARN defaults, exit 0.
func TestDoctorWarnsOnMissingStoreAndConfig(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("doctor: code = %d, want 0; stdout = %q", code, stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{"[WARN] config", "[WARN] slice store", "[WARN] embedder", "[PASS] judge"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "checks: pass=1 warn=3 fail=0") {
		t.Errorf("stdout missing summary:\n%s", out)
	}
}

// TestDoctorFailsOnCorruptStore: one truncated JSONL line -> FAIL + exit 3,
// with the corrupt line number reported.
func TestDoctorFailsOnCorruptStore(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	dbPath := filepath.Join(t.TempDir(), "broken.db")
	writeJSONL(t, dbPath,
		sliceJSON(t, "ok"),
		`{"ID":"broken"`, // truncated JSON
		sliceJSON(t, "ok2"))
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--db", dbPath}, &stdout, &stderr, productionDependencies())
	if code != 3 {
		t.Fatalf("doctor: code = %d, want 3; stdout = %q", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[FAIL] slice store") || !strings.Contains(out, "corrupt JSONL line(s) [2]") {
		t.Errorf("stdout missing corrupt-store report:\n%s", out)
	}
	if !strings.Contains(out, "checks: pass=1 warn=2 fail=1") {
		t.Errorf("stdout missing summary:\n%s", out)
	}
}

// TestDoctorFailsOnDirAsStore: --db pointing at a directory -> FAIL + exit 3.
func TestDoctorFailsOnDirAsStore(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--db", dir}, &stdout, &stderr, productionDependencies())
	if code != 3 {
		t.Fatalf("doctor: code = %d, want 3; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] slice store") {
		t.Errorf("stdout missing dir-store report:\n%s", stdout.String())
	}
}

// TestDoctorFailsOnBadConfig: explicit --config with a syntax error ->
// FAIL + exit 3.
func TestDoctorFailsOnBadConfig(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	cfgPath := writeConfig(t, "db = \n") // empty value
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--config", cfgPath}, &stdout, &stderr, productionDependencies())
	if code != 3 {
		t.Fatalf("doctor: code = %d, want 3; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] config") {
		t.Errorf("stdout missing config report:\n%s", stdout.String())
	}
}

// TestDoctorFailsOnMissingExplicitConfig: --config pointing at a missing
// file is a FAIL (an explicit path must exist), unlike the default path.
func TestDoctorFailsOnMissingExplicitConfig(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	missing := filepath.Join(t.TempDir(), "nope.toml")
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--config", missing}, &stdout, &stderr, productionDependencies())
	if code != 3 {
		t.Fatalf("doctor: code = %d, want 3; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "config file missing") {
		t.Errorf("stdout missing missing-config report:\n%s", stdout.String())
	}
}

// TestDoctorEmbedderPartialConfig: [embed] without model and without env ->
// FAIL + exit 3, listing exactly what is missing.
func TestDoctorEmbedderPartialConfig(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	cfgPath := writeConfig(t, "[embed]\nbase_url = \"https://api.openai.com/v1\"\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--config", cfgPath}, &stdout, &stderr, productionDependencies())
	if code != 3 {
		t.Fatalf("doctor: code = %d, want 3; stdout = %q", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[FAIL] embedder") || !strings.Contains(out, "SEMANTIX_EMBED_MODEL") ||
		!strings.Contains(out, "SEMANTIX_EMBED_API_KEY") {
		t.Errorf("stdout missing embedder report:\n%s", out)
	}
}

// TestDoctorJudgePartialConfig: [judge] endpoint without the env API key ->
// FAIL + exit 3; a bad protocol is also a FAIL.
func TestDoctorJudgePartialConfig(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	cfgPath := writeConfig(t, "[judge]\nbase_url = \"https://api.openai.com/v1\"\nmodel = \"gpt-4o-mini\"\nprotocol = \"openai\"\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--config", cfgPath}, &stdout, &stderr, productionDependencies())
	if code != 3 {
		t.Fatalf("doctor: code = %d, want 3; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "SEMANTIX_JUDGE_API_KEY") {
		t.Errorf("stdout missing judge report:\n%s", stdout.String())
	}

	badProtocol := writeConfig(t, "[judge]\nbase_url = \"https://api.openai.com/v1\"\nmodel = \"m\"\nprotocol = \"xml\"\n")
	t.Setenv("SEMANTIX_JUDGE_API_KEY", "sk-test")
	var stdout2, stderr2 bytes.Buffer
	if code := run([]string{"doctor", "--config", badProtocol}, &stdout2, &stderr2, productionDependencies()); code != 3 {
		t.Fatalf("doctor (bad protocol): code = %d, want 3; stdout = %q", code, stdout2.String())
	}
	if !strings.Contains(stdout2.String(), "invalid judge protocol") {
		t.Errorf("stdout missing protocol report:\n%s", stdout2.String())
	}
}

// TestDoctorJSON: --json emits the §4.2 envelope; a failing workspace has
// ok=false + fail count in data and still exits 3.
func TestDoctorJSON(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)

	// Failing workspace: corrupt store, no config.
	dbPath := filepath.Join(t.TempDir(), "broken.db")
	writeJSONL(t, dbPath, `{"ID":"broken"`)
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--db", dbPath, "--json"}, &stdout, &stderr, productionDependencies())
	if code != 3 {
		t.Fatalf("doctor --json: code = %d, want 3; stdout = %q", code, stdout.String())
	}
	var env doctorEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("doctor --json: unparseable output %q: %v", stdout.String(), err)
	}
	if env.OK || env.Command != "doctor" || env.Version != cliVersion {
		t.Fatalf("envelope = %+v, want ok=false command=doctor version=%s", env, cliVersion)
	}
	if env.Error == nil {
		t.Fatalf("envelope.error = nil, want {code:3,message} on failure (§4.2)")
	}
	if errEnv, ok := env.Error.(map[string]any); !ok || errEnv["code"] != float64(3) {
		t.Fatalf("envelope.error = %v, want code 3", env.Error)
	}
	if len(env.Data.Checks) != 4 || env.Data.Fail == 0 || env.Data.DB != dbPath {
		t.Fatalf("data = %+v", env.Data)
	}

	// Healthy workspace: ok=true, exit 0.
	okDB := filepath.Join(t.TempDir(), "ok.db")
	writeJSONL(t, okDB, sliceJSON(t, "a"))
	var stdout2, stderr2 bytes.Buffer
	if code := run([]string{"doctor", "--db", okDB, "--json"}, &stdout2, &stderr2, productionDependencies()); code != 0 {
		t.Fatalf("doctor --json (healthy): code = %d, want 0; stdout = %q", code, stdout2.String())
	}
	if err := json.Unmarshal(stdout2.Bytes(), &env); err != nil {
		t.Fatalf("doctor --json (healthy): unparseable output: %v", err)
	}
	if !env.OK || env.Data.Fail != 0 {
		t.Fatalf("envelope = %+v, want ok=true fail=0", env)
	}
}

// TestDoctorUsageErrors: unknown flags and stray positional args are usage
// errors (exit 2); --help is a successful request.
func TestDoctorUsageErrors(t *testing.T) {
	clearDoctorEnv(t)
	for _, args := range [][]string{
		{"doctor", "--bogus"},
		{"doctor", "stray-positional"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, productionDependencies()); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"doctor", "--help"}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Errorf("doctor --help = %d, want 0", code)
	}
}

// TestDoctorEmptyStoreWarns: an existing but empty store is a WARN (it is
// created on first extract), not a FAIL.
func TestDoctorEmptyStoreWarns(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	writeJSONL(t, dbPath) // zero lines
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--db", dbPath}, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("doctor with empty store: code = %d, want 0; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "[WARN] slice store") || !strings.Contains(stdout.String(), "store is empty") {
		t.Fatalf("missing empty-store WARN:\n%s", stdout.String())
	}
}

// TestDoctorAcceptsEmptyStringValues: `db = ""` is legal TOML; the parser
// must distinguish an empty raw value (`db =`, a syntax error) from a
// quoted empty string (`db = ""`, a valid empty value).
func TestDoctorAcceptsEmptyStringValues(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	cfgPath := writeConfig(t, "[store]\ndb = \"\"\nscope = \"project\"\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--config", cfgPath}, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("doctor with db = \"\": code = %d, want 0; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] config") {
		t.Fatalf("config must PASS:\n%s", stdout.String())
	}
}

// TestDoctorJudgeMissingProtocol: an endpoint without [judge] protocol is a
// FAIL — kernel/judge.NewLLMJudge rejects an empty protocol, so doctor must
// not certify it (review M1).
func TestDoctorJudgeMissingProtocol(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	t.Setenv("SEMANTIX_JUDGE_API_KEY", "sk-test")
	cfgPath := writeConfig(t, "[judge]\nbase_url = \"https://api.openai.com/v1\"\nmodel = \"gpt-4o-mini\"\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--config", cfgPath}, &stdout, &stderr, productionDependencies())
	if code != 3 {
		t.Fatalf("doctor: code = %d, want 3; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "judge protocol missing") {
		t.Errorf("stdout missing protocol report:\n%s", stdout.String())
	}
}

// TestDoctorJudgeKeyOnlyEnv: a stray SEMANTIX_JUDGE_API_KEY without any
// endpoint is a WARN (the key is unused), not a FAIL (review m4).
func TestDoctorJudgeKeyOnlyEnv(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	t.Setenv("SEMANTIX_JUDGE_API_KEY", "sk-test")
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("doctor: code = %d, want 0; stdout = %q", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[WARN] judge") || !strings.Contains(out, "key is unused") {
		t.Errorf("stdout missing key-only judge WARN:\n%s", out)
	}
}

// TestDoctorConfigEdgeCases: the TOML subset parser must survive real-world
// files — a UTF-8 BOM (PowerShell), escaped quotes with an inline comment,
// single-quoted literal paths, and very long comment lines (review M2/M3,
// m2/m3).
func TestDoctorConfigEdgeCases(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)

	// BOM + inline comment + single-quoted literal scope.
	bomConfig := "\ufeff[store]\ndb = \"never.db\" # trailing comment\nscope = 'project'\n"
	cfgPath := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(cfgPath, []byte(bomConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--config", cfgPath}, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("doctor (BOM/escapes): code = %d, want 0; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] config") {
		t.Errorf("config must PASS:\n%s", stdout.String())
	}

	// A huge pure-comment line must not trip the scanner token limit.
	longComment := "[store]\n# " + strings.Repeat("x", 100*1024) + "\ndb = \"never.db\"\n"
	longPath := filepath.Join(t.TempDir(), "long.toml")
	if err := os.WriteFile(longPath, []byte(longComment), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout2, stderr2 bytes.Buffer
	if code := run([]string{"doctor", "--config", longPath}, &stdout2, &stderr2, productionDependencies()); code != 0 {
		t.Fatalf("doctor (long comment): code = %d, want 0; stdout = %q", code, stdout2.String())
	}
	if !strings.Contains(stdout2.String(), "[PASS] config") {
		t.Errorf("config must PASS with long comments:\n%s", stdout2.String())
	}
}

// TestParseDoctorConfigEscapedQuotes: `#` inside a basic string with an
// escaped quote must survive comment stripping, and the escaped quote must
// resolve (review M2). A value containing a quote is never a usable path on
// Windows, so this unit test pins the parser without touching the store.
func TestParseDoctorConfigEscapedQuotes(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cfg.toml")
	content := "[embed]\nmodel = \"a\\\"#b\" # trailing comment\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := parseDoctorConfig(cfgPath)
	if err != nil {
		t.Fatalf("parseDoctorConfig: %v", err)
	}
	if cfg.embedModel != `a"#b` {
		t.Errorf("model = %q, want %q", cfg.embedModel, `a"#b`)
	}
}

// TestDoctorHelpShowsProductCommands: mounting doctor/completion/install must
// not hide the other product-group commands from `semantix help` (review m6).
func TestDoctorHelpShowsPlanned(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("help: code = %d", code)
	}
	if !strings.Contains(stdout.String(), "Product & management") ||
		!strings.Contains(stdout.String(), "completion") ||
		!strings.Contains(stdout.String(), "install") ||
		!strings.Contains(stdout.String(), "version") ||
		!strings.Contains(stdout.String(), "config") ||
		!strings.Contains(stdout.String(), "init") {
		t.Errorf("help must show product-group commands:\n%s", stdout.String())
	}
}

// TestDoctorEnvOverridesConfig: SEMANTIX_DB / SEMANTIX_CONFIG env overrides
// beat the config file and the built-in defaults.
func TestDoctorEnvOverridesConfig(t *testing.T) {
	clearDoctorEnv(t)
	chdirTemp(t)
	dbPath := filepath.Join(t.TempDir(), "env.db")
	writeJSONL(t, dbPath, sliceJSON(t, "a"))
	cfgPath := writeConfig(t, "[store]\ndb = \".semantix/project.db\"\n") // different db than env
	t.Setenv("SEMANTIX_DB", dbPath)
	t.Setenv("SEMANTIX_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("doctor: code = %d, want 0; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] slice store") || strings.Contains(stdout.String(), ".semantix/project.db") {
		t.Errorf("env override not applied:\n%s", stdout.String())
	}
}
