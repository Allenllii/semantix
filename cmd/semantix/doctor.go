package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"

	"semantix/kernel/slice"
)

// cliVersion is the semantix CLI release reported in the --json envelope
// (docs/reports/cli-v2-architecture.md §4.2). Bump it with each release tag.
const cliVersion = "0.3.1"

// doctorStatus is one health-check outcome.
type doctorStatus string

const (
	statusPass doctorStatus = "pass"
	statusWarn doctorStatus = "warn"
	statusFail doctorStatus = "fail"
)

// doctorCheck is one row of the U24 report (Issue #118): a machine-readable
// id, the PASS/WARN/FAIL outcome, human detail, and the fix to apply.
type doctorCheck struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Status doctorStatus `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Advice string       `json:"advice,omitempty"`
}

// doctorReport is the `data` payload of `semantix doctor --json`.
type doctorReport struct {
	Config string        `json:"config"`
	DB     string        `json:"db"`
	Checks []doctorCheck `json:"checks"`
	Pass   int           `json:"pass"`
	Warn   int           `json:"warn"`
	Fail   int           `json:"fail"`
}

// doctorEnvelope is the §4.2 JSON envelope: ok + command + data + error +
// version. ok is false iff any check FAILed (exit code 3 still applies).
type doctorEnvelope struct {
	OK      bool         `json:"ok"`
	Command string       `json:"command"`
	Data    doctorReport `json:"data"`
	Error   any          `json:"error"`
	Version string       `json:"version"`
}

// runDoctor implements the U24 health check (Issue #118): one command that
// diagnoses the failure modes which previously surfaced only at runtime — an
// unreadable/corrupt slice store, an unparseable config, a missing
// embedder, and an invalid judge setup. Every check prints
// PASS/WARN/FAIL + remediation; any FAIL makes doctor exit 3 (gate,
// docs/reports/cli-v2-architecture.md §4.3).
func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "config file path (default: ./semantix.toml; skipped when absent)")
	dbFlag := fs.String("db", "", "slice store path (overrides SEMANTIX_DB and [store] db)")
	jsonOutput := fs.Bool("json", false, "write the report as JSON (envelope per cli-v2-architecture §4.2)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "Usage: semantix doctor [--config <path>] [--db <path>] [--json]")
		return 2
	}

	// Config path: --config > SEMANTIX_CONFIG > default (not found = WARN).
	cfgPath := *configPath
	explicit := cfgPath != ""
	if cfgPath == "" {
		if v := os.Getenv("SEMANTIX_CONFIG"); v != "" {
			cfgPath, explicit = v, true
		} else {
			cfgPath = "semantix.toml"
		}
	}
	cfgCheck, cfg := checkDoctorConfig(cfgPath, explicit)

	// Store path: --db > SEMANTIX_DB > [store] db > scope default.
	dbPath := *dbFlag
	if dbPath == "" {
		if v := os.Getenv("SEMANTIX_DB"); v != "" {
			dbPath = v
		} else if cfg != nil && cfg.db != "" {
			dbPath = cfg.db
		} else if cfg != nil && cfg.scope == "user" {
			dbPath = defaultUserDB()
		} else {
			dbPath = defaultProjectDB()
		}
	}

	report := doctorReport{
		Config: cfgPath,
		DB:     dbPath,
		Checks: []doctorCheck{
			cfgCheck,
			checkDoctorDB(dbPath),
			checkDoctorEmbedder(cfg),
			checkDoctorJudge(cfg),
		},
	}
	summarizeDoctor(&report)

	if *jsonOutput {
		if err := emitDoctorJSON(stdout, report); err != nil {
			fmt.Fprintf(os.Stderr, "semantix doctor: %v\n", err)
			return 1
		}
	} else {
		printDoctorReport(stdout, report)
	}
	if report.Fail > 0 {
		return 3
	}
	return 0
}

// summarizeDoctor tallies the per-check statuses into the report header.
func summarizeDoctor(r *doctorReport) {
	for _, c := range r.Checks {
		switch c.Status {
		case statusPass:
			r.Pass++
		case statusWarn:
			r.Warn++
		case statusFail:
			r.Fail++
		}
	}
}

// printDoctorReport renders the human-readable report. Status labels are
// uppercased so PASS/WARN/FAIL are grep-able in CI logs.
func printDoctorReport(w io.Writer, r doctorReport) {
	fmt.Fprintf(w, "# semantix doctor v%s · config=%s · db=%s\n", cliVersion, r.Config, r.DB)
	for _, c := range r.Checks {
		fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(string(c.Status)), c.Name, c.Detail)
		if c.Advice != "" {
			fmt.Fprintf(w, "      -> %s\n", c.Advice)
		}
	}
	fmt.Fprintf(w, "checks: pass=%d warn=%d fail=%d\n", r.Pass, r.Warn, r.Fail)
}

// envelopeError is the §4.2 failure payload: ok=false + error{code,message}.
type envelopeError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// emitDoctorJSON writes the §4.2 envelope. ok=false whenever any check
// failed; the exit code (3) is reported separately by the caller. On
// failure the error field carries {code,message} of the first FAIL (the
// per-check details stay in data.checks).
func emitDoctorJSON(w io.Writer, report doctorReport) error {
	env := doctorEnvelope{
		OK:      report.Fail == 0,
		Command: "doctor",
		Data:    report,
		Version: cliVersion,
	}
	if report.Fail > 0 {
		msg := "health check failed"
		for _, c := range report.Checks {
			if c.Status == statusFail {
				msg = c.Detail
				break
			}
		}
		env.Error = envelopeError{Code: 3, Message: msg}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // advice strings may contain < > &; keep them readable
	return enc.Encode(env)
}

// doctorConfig is the TOML subset doctor consumes. It is deliberately
// self-contained (U20 will own full config loading later): doctor only
// needs the keys that decide whether the workspace can run.
type doctorConfig struct {
	sections      []string // section names in file order
	db            string   // [store] db
	scope         string   // [store] scope
	embedBaseURL  string   // [embed] base_url
	embedModel    string   // [embed] model
	judgeBaseURL  string   // [judge] base_url
	judgeModel    string   // [judge] model
	judgeProtocol string   // [judge] protocol
}

// parseDoctorConfig reads the TOML subset doctor needs: [section] headers
// and key = value lines with # comments; values may be quoted strings or
// bare tokens. Unknown sections and keys are ignored (forward-compatible
// with the full config U20 wires). Syntax errors are fatal.
func parseDoctorConfig(path string) (*doctorConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer f.Close()

	cfg := &doctorConfig{}
	seen := map[string]bool{}
	section := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // tolerate 8 MiB lines (e.g. a long comment)
	for lineNo := 1; sc.Scan(); lineNo++ {
		// Strip a UTF-8 BOM: PowerShell 5.1 writes one on Set-Content and
		// TrimSpace does not remove it, which would break line 1.
		line := strings.TrimPrefix(strings.TrimSpace(stripTOMLComment(sc.Text())), "\ufeff")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section != "" && !seen[section] {
				seen[section] = true
				cfg.sections = append(cfg.sections, section)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected key = value", path, lineNo)
		}
		key = strings.TrimSpace(key)
		// `db =` (empty raw value) is a syntax error; `db = ""` is a legal
		// empty string — judge on the raw value before unquoting.
		raw := strings.TrimSpace(value)
		if key == "" || raw == "" {
			return nil, fmt.Errorf("%s:%d: empty key or value", path, lineNo)
		}
		value = unquote(raw)
		switch section {
		case "store":
			switch key {
			case "db":
				cfg.db = value
			case "scope":
				cfg.scope = value
			}
		case "embed":
			switch key {
			case "base_url":
				cfg.embedBaseURL = value
			case "model":
				cfg.embedModel = value
			}
		case "judge":
			switch key {
			case "base_url":
				cfg.judgeBaseURL = value
			case "model":
				cfg.judgeModel = value
			case "protocol":
				cfg.judgeProtocol = value
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: read: %w", path, err)
	}
	return cfg, nil
}

// stripTOMLComment removes a # comment, keeping # inside quoted strings.
// Both TOML quote styles are tracked: "basic" strings (where a backslash
// escapes the next character, so \" must not toggle the quote state) and
// 'literal' strings (no escapes at all).
func stripTOMLComment(line string) string {
	inQuote := byte(0) // 0 = outside, '"' or '\'' = inside
	for i := 0; i < len(line); i++ {
		switch {
		case inQuote == '"' && line[i] == '\\':
			i++ // escaped character inside a basic string
		case line[i] == '"' || line[i] == '\'':
			if inQuote == line[i] {
				inQuote = 0
			} else if inQuote == 0 {
				inQuote = line[i]
			}
		case line[i] == '#' && inQuote == 0:
			return line[:i]
		}
	}
	return line
}

// unquote strips surrounding quotes: "basic" strings resolve the two
// escapes a path/URL might contain (\" and \\); 'literal' strings pass
// through verbatim. Unquoted (bare) values pass through.
func unquote(v string) string {
	switch {
	case len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"':
		inner := v[1 : len(v)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		inner = strings.ReplaceAll(inner, `\\`, `\`)
		return inner
	case len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'':
		return v[1 : len(v)-1]
	}
	return v
}

// checkDoctorConfig validates the config file. An explicitly requested path
// (--config / SEMANTIX_CONFIG) must exist and parse; a missing default path
// is only a warning because built-in defaults are valid. Parse failures are
// FAILs — a broken config would silently drop settings later.
func checkDoctorConfig(path string, explicit bool) (doctorCheck, *doctorConfig) {
	cfg, err := parseDoctorConfig(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !explicit {
			return doctorCheck{
				ID: "config", Name: "config", Status: statusWarn,
				Detail: fmt.Sprintf("%s: no config file; built-in defaults apply", path),
				Advice: "copy semantix.example.toml to semantix.toml to pin db / embed / judge settings",
			}, nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return doctorCheck{
				ID: "config", Name: "config", Status: statusFail,
				Detail: fmt.Sprintf("%s: config file missing", path),
				Advice: "create it from semantix.example.toml, or drop the explicit --config / SEMANTIX_CONFIG",
			}, nil
		}
		return doctorCheck{
			ID: "config", Name: "config", Status: statusFail,
			Detail: fmt.Sprintf("%s: %v", path, err),
			Advice: "fix the file (line/col reported above) and re-run",
		}, nil
	}
	detail := fmt.Sprintf("%s: parsed", path)
	if len(cfg.sections) > 0 {
		detail += " (sections: " + strings.Join(cfg.sections, ", ") + ")"
	}
	return doctorCheck{
		ID: "config", Name: "config", Status: statusPass, Detail: detail,
	}, cfg
}

// checkDoctorDB verifies the slice store is readable and every JSONL line
// parses as a Slice. A missing or empty store is only a warning (it is
// created on first extract); corruption is a FAIL because FileStore silently
// skips corrupt lines on read, so an unaddressed store degrades invisibly.
func checkDoctorDB(dbPath string) doctorCheck {
	info, err := os.Stat(dbPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return doctorCheck{
				ID: "db", Name: "slice store", Status: statusWarn,
				Detail: fmt.Sprintf("%s: no store yet", dbPath),
				Advice: "run `semantix extract --input <session.jsonl>` to build the slice library",
			}
		}
		return doctorCheck{
			ID: "db", Name: "slice store", Status: statusFail,
			Detail: fmt.Sprintf("%s: not readable: %v", dbPath, err),
			Advice: "fix file permissions or restore from backup",
		}
	}
	if info.IsDir() {
		return doctorCheck{
			ID: "db", Name: "slice store", Status: statusFail,
			Detail: fmt.Sprintf("%s: is a directory, not a JSONL store", dbPath),
			Advice: "point --db / [store] db at the slice file (default .semantix/project.db)",
		}
	}
	f, err := os.Open(dbPath)
	if err != nil {
		return doctorCheck{
			ID: "db", Name: "slice store", Status: statusFail,
			Detail: fmt.Sprintf("%s: not readable: %v", dbPath, err),
			Advice: "fix file permissions or restore from backup",
		}
	}
	defer f.Close()

	valid, total := 0, 0
	var corrupt []int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // tolerate slices up to 8 MiB
	for sc.Scan() {
		total++
		// A UTF-8 BOM on the first line (PowerShell Set-Content) would
		// corrupt an otherwise valid slice; strip it before unmarshal.
		line := bytes.TrimPrefix(bytes.TrimSpace(sc.Bytes()), []byte("\ufeff"))
		if len(line) == 0 {
			continue
		}
		var sl slice.Slice
		if err := json.Unmarshal(line, &sl); err != nil || sl.ID == "" {
			corrupt = append(corrupt, total)
			continue
		}
		valid++
	}
	if sc.Err() != nil {
		corrupt = append(corrupt, total+1)
	}
	if len(corrupt) > 0 {
		return doctorCheck{
			ID: "db", Name: "slice store", Status: statusFail,
			Detail: fmt.Sprintf("%s: %d corrupt JSONL line(s) %v (%d valid)", dbPath, len(corrupt), corrupt, valid),
			Advice: "drop the listed lines or restore the store from a backup",
		}
	}
	if valid == 0 {
		return doctorCheck{
			ID: "db", Name: "slice store", Status: statusWarn,
			Detail: fmt.Sprintf("%s: store is empty", dbPath),
			Advice: "run `semantix extract --input <session.jsonl>` to add slices",
		}
	}
	return doctorCheck{
		ID: "db", Name: "slice store", Status: statusPass,
		Detail: fmt.Sprintf("%s: readable, %d slices, JSONL complete", dbPath, valid),
	}
}

// resolveEmbed merges [embed] config with SEMANTIX_EMBED_* env overrides.
// The API key is env-only (never stored in config or code, Issue #63).
func resolveEmbed(cfg *doctorConfig) (baseURL, model, key string) {
	if cfg != nil {
		baseURL, model = cfg.embedBaseURL, cfg.embedModel
	}
	if v := os.Getenv("SEMANTIX_EMBED_BASE_URL"); v != "" {
		baseURL = v
	}
	if v := os.Getenv("SEMANTIX_EMBED_MODEL"); v != "" {
		model = v
	}
	key = os.Getenv("SEMANTIX_EMBED_API_KEY")
	return
}

// checkDoctorEmbedder reports whether a model embedder is wired. No wiring
// at all is a WARN (search degrades to hash vectors + BM25, which works);
// a partial wiring is a FAIL because the model path is configured but can
// never run. No network call is made: doctor must stay fast and offline.
func checkDoctorEmbedder(cfg *doctorConfig) doctorCheck {
	baseURL, model, key := resolveEmbed(cfg)
	missing := missingArgs(map[string]string{
		"SEMANTIX_EMBED_BASE_URL (or [embed] base_url)": baseURL,
		"SEMANTIX_EMBED_MODEL (or [embed] model)":       model,
		"SEMANTIX_EMBED_API_KEY":                         key,
	})
	switch len(missing) {
	case 0:
		return doctorCheck{
			ID: "embedder", Name: "embedder", Status: statusPass,
			Detail: fmt.Sprintf("model embedder configured (%s)", model),
		}
	case 3:
		return doctorCheck{
			ID: "embedder", Name: "embedder", Status: statusWarn,
			Detail: "Noop embedder: no model wired — vector retrieval falls back to hash vectors + BM25",
			Advice: "set SEMANTIX_EMBED_BASE_URL / SEMANTIX_EMBED_MODEL / SEMANTIX_EMBED_API_KEY (or [embed] base_url / model) to enable semantic vectors",
		}
	default:
		return doctorCheck{
			ID: "embedder", Name: "embedder", Status: statusFail,
			Detail: "embedder configured but incomplete; missing: " + strings.Join(missing, ", "),
			Advice: "set the missing values (env overrides config)",
		}
	}
}

// resolveJudge merges [judge] config with the env API key. Like the
// embedder, the judge key is env-only (SEMANTIX_JUDGE_API_KEY).
func resolveJudge(cfg *doctorConfig) (baseURL, model, protocol, key string) {
	if cfg != nil {
		baseURL, model, protocol = cfg.judgeBaseURL, cfg.judgeModel, cfg.judgeProtocol
	}
	key = os.Getenv("SEMANTIX_JUDGE_API_KEY")
	return
}

// checkDoctorJudge validates the judge setup only when one is configured
// ("judge 配置有效（若配置）"): unconfigured is a PASS because the grey zone
// then falls back to the conservative reject, which is the safe default. A
// stray API key without an endpoint is a WARN (it is simply unused). A
// configured endpoint that cannot run — missing pieces, empty or invalid
// protocol — is a FAIL, matching kernel/judge.NewLLMJudge which rejects
// such configs at runtime.
func checkDoctorJudge(cfg *doctorConfig) doctorCheck {
	baseURL, model, protocol, key := resolveJudge(cfg)
	endpointConfigured := baseURL != "" || model != "" || protocol != ""
	switch {
	case !endpointConfigured && key == "":
		return doctorCheck{
			ID: "judge", Name: "judge", Status: statusPass,
			Detail: "no judge configured — grey-zone candidates are conservatively rejected (safe default)",
			Advice: "to enable the LLM judge set [judge] base_url / model / protocol and SEMANTIX_JUDGE_API_KEY",
		}
	case !endpointConfigured:
		return doctorCheck{
			ID: "judge", Name: "judge", Status: statusWarn,
			Detail: "SEMANTIX_JUDGE_API_KEY set but no [judge] endpoint configured — the key is unused",
			Advice: "set [judge] base_url / model / protocol, or unset the key",
		}
	case protocol == "":
		return doctorCheck{
			ID: "judge", Name: "judge", Status: statusFail,
			Detail: "judge protocol missing (want openai or anthropic)",
			Advice: "add [judge] protocol = \"openai\" (or \"anthropic\")",
		}
	case protocol != "openai" && protocol != "anthropic":
		return doctorCheck{
			ID: "judge", Name: "judge", Status: statusFail,
			Detail: fmt.Sprintf("invalid judge protocol %q (want openai or anthropic)", protocol),
			Advice: "fix [judge] protocol",
		}
	}
	missing := missingArgs(map[string]string{
		"judge base_url ([judge] base_url)": baseURL,
		"judge model ([judge] model)":       model,
		"SEMANTIX_JUDGE_API_KEY":             key,
	})
	if len(missing) > 0 {
		return doctorCheck{
			ID: "judge", Name: "judge", Status: statusFail,
			Detail: "judge configured but incomplete; missing: " + strings.Join(missing, ", "),
			Advice: "set the missing values; the API key is env-only (SEMANTIX_JUDGE_API_KEY)",
		}
	}
	return doctorCheck{
		ID: "judge", Name: "judge", Status: statusPass,
		Detail: fmt.Sprintf("judge ready (%s, %s)", protocol, model),
	}
}

// missingArgs returns the sorted names of required settings with empty
// values, so remediation messages list exactly what is missing.
func missingArgs(required map[string]string) []string {
	var missing []string
	for name, val := range required {
		if strings.TrimSpace(val) == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}
