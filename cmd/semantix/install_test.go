package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillPayload creates a minimal agent-skill source tree mirroring the
// real repo layout (SKILL.md, tools/, hooks/, config/, scripts/).
func writeSkillPayload(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"SKILL.md":                     "# semantix skill\n\n> install me\n",
		"tools/semantix-lookup.md":     "# semantix_lookup tool\n\n```json\n{\"name\":\"semantix_lookup\"}\n```\n",
		"hooks/session-bypass.md":      "# session bypass\n\nExport JSONL and run extract.\n",
		"config/semantix.example.toml": "[store]\ndb = \"~/.semantix/user.db\"\n",
		"scripts/install.sh":           "#!/usr/bin/env bash\nset -euo pipefail\necho install\n",
		"scripts/selftest.sh":          "#!/usr/bin/env bash\necho SELFTEST PASS\n",
	}
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// installRuns is the common arrange for install tests: a payload source plus
// a destination inside t.TempDir.
func installRuns(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = runInstall(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestInstallUsageContract(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no target", []string{}, 2},
		{"invalid target", []string{"--target", "vscode"}, 2},
		{"custom without --dir", []string{"--target", "custom", "--source", "x"}, 2},
		{"extra positional", []string{"--target", "claude-code", "extra"}, 2},
		{"unknown flag", []string{"--bogus"}, 2},
		{"help exits zero", []string{"--help"}, 0},
	}
	for _, c := range cases {
		code, _, stderr := installRuns(t, c.args...)
		if code != c.want {
			t.Errorf("%s: code = %d, want %d (stderr %q)", c.name, code, c.want, stderr)
		}
	}
}

func TestInstallCopiesPayload(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")

	code, stdout, stderr := installRuns(t,
		"--target", "claude-code", "--dir", dest, "--source", src)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	want := []string{
		"SKILL.md",
		"tools/semantix-lookup.md",
		"hooks/session-bypass.md",
		"config/semantix.example.toml",
		"scripts/install.sh",
		"scripts/selftest.sh",
	}
	for _, rel := range want {
		path := filepath.Join(dest, filepath.FromSlash(rel))
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("payload file %q not written: %v", rel, err)
		}
		srcContent, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(content, srcContent) {
			t.Errorf("%s: content mismatch", rel)
		}
	}
	// Acceptance: output references agent-skill/SKILL.md and
	// hooks/session-bypass.md (next steps point at the installed copies).
	for _, rel := range []string{"SKILL.md", "hooks/session-bypass.md"} {
		if !strings.Contains(stdout, rel) {
			t.Errorf("stdout does not reference %s:\n%s", rel, stdout)
		}
	}
	for _, rel := range want {
		if !strings.Contains(stdout, "[installed] "+filepath.ToSlash(rel)) {
			t.Errorf("stdout missing [installed] for %s:\n%s", rel, stdout)
		}
	}
	// Manifest written with the file list.
	manifest := readInstallManifest(t, dest)
	if len(manifest.Files) != len(want) {
		t.Fatalf("manifest files = %v, want %d", manifest.Files, len(want))
	}
	for _, rel := range want {
		found := false
		for _, m := range manifest.Files {
			if m == filepath.ToSlash(rel) {
				found = true
			}
		}
		if !found {
			t.Errorf("manifest missing %s (got %v)", rel, manifest.Files)
		}
	}
}

func TestInstallIdempotent(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")

	args := []string{"--target", "semantix", "--dir", dest, "--source", src}
	code, _, stderr := installRuns(t, args...)
	if code != 0 {
		t.Fatalf("first run code = %d, stderr = %q", code, stderr)
	}
	before, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := installRuns(t, args...)
	if code != 0 {
		t.Fatalf("second run code = %d, stderr = %q", code, stderr)
	}
	after, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("second run modified an unchanged file")
	}
	if strings.Contains(stdout, "[installed]") || strings.Contains(stdout, "[updated]") {
		t.Fatalf("second run must be a no-op, got:\n%s", stdout)
	}
	for _, rel := range []string{"SKILL.md", "tools/semantix-lookup.md", "hooks/session-bypass.md"} {
		if !strings.Contains(stdout, "[unchanged] "+filepath.ToSlash(rel)) {
			t.Errorf("stdout missing [unchanged] for %s:\n%s", rel, stdout)
		}
	}
}

func TestInstallUpdateOverwritesChangedFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")

	args := []string{"--target", "claude-code", "--dir", dest, "--source", src}
	if code, _, stderr := installRuns(t, args...); code != 0 {
		t.Fatalf("first run code = %d, stderr = %q", code, stderr)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := installRuns(t, args...)
	if code != 0 {
		t.Fatalf("second run code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "[updated] SKILL.md") {
		t.Fatalf("stdout missing [updated] SKILL.md:\n%s", stdout)
	}
	content, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# v2\n" {
		t.Fatalf("SKILL.md = %q, want updated content", content)
	}
}

func TestInstallUninstallRemovesTrackedFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")

	if code, _, stderr := installRuns(t, "--target", "claude-code", "--dir", dest, "--source", src); code != 0 {
		t.Fatalf("install code = %d, stderr = %q", code, stderr)
	}

	code, stdout, stderr := installRuns(t, "--target", "claude-code", "--dir", dest, "--uninstall")
	if code != 0 {
		t.Fatalf("uninstall code = %d, stderr = %q", code, stderr)
	}
	for _, rel := range []string{
		"SKILL.md", "tools/semantix-lookup.md", "hooks/session-bypass.md",
		"config/semantix.example.toml", "scripts/install.sh", "scripts/selftest.sh",
	} {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s still exists after uninstall", rel)
		}
		if !strings.Contains(stdout, "[removed] "+filepath.ToSlash(rel)) {
			t.Errorf("stdout missing [removed] for %s:\n%s", rel, stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, installManifestName)); !os.IsNotExist(err) {
		t.Error("manifest still exists after uninstall")
	}
	// Idempotent: a second --uninstall is a safe no-op.
	code, stdout, stderr = installRuns(t, "--target", "claude-code", "--dir", dest, "--uninstall")
	if code != 0 {
		t.Fatalf("second uninstall code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "nothing tracked") {
		t.Fatalf("second uninstall should report nothing tracked:\n%s", stdout)
	}
}

func TestInstallUninstallKeepsForeignFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")

	if code, _, stderr := installRuns(t, "--target", "custom", "--dir", dest, "--source", src); code != 0 {
		t.Fatalf("install code = %d, stderr = %q", code, stderr)
	}
	foreign := filepath.Join(dest, "my-notes.txt")
	if err := os.WriteFile(foreign, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := installRuns(t, "--target", "custom", "--dir", dest, "--uninstall"); code != 0 {
		t.Fatalf("uninstall code = %d, stderr = %q", code, stderr)
	}
	if content, err := os.ReadFile(foreign); err != nil || string(content) != "user data" {
		t.Fatalf("foreign file was touched: content = %q, err = %v", content, err)
	}
}

func TestInstallJSONEnvelope(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")

	code, stdout, stderr := installRuns(t,
		"--target", "semantix", "--dir", dest, "--source", src, "--json")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	var env installEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if !env.OK || env.Command != "install" || env.Version != cliVersion {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Data.Target != "semantix" || env.Data.Dest != dest || len(env.Data.Files) == 0 {
		t.Fatalf("data = %+v", env.Data)
	}
	if len(env.Data.Next) == 0 {
		t.Fatal("semantix next steps missing (must reference SKILL.md / hooks/session-bypass.md)")
	}
	if !strings.Contains(env.Data.Next[0], "SKILL.md") {
		t.Fatalf("next steps do not reference SKILL.md: %v", env.Data.Next)
	}
}

func TestInstallMissingSource(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dest")
	code, _, stderr := installRuns(t,
		"--target", "custom", "--dir", dest, "--source", filepath.Join(t.TempDir(), "nope"))
	if code != 1 {
		t.Fatalf("code = %d, want 1 (stderr %q)", code, stderr)
	}
}

func TestInstallSourceEnvFallback(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")
	t.Setenv("SEMANTIX_SKILL_DIR", src)

	code, _, stderr := installRuns(t, "--target", "claude-code", "--dir", dest)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
		t.Fatal("payload not installed via SEMANTIX_SKILL_DIR")
	}
}

// TestInstallExplicitSourceIsFinal: an invalid --source must fail even when
// another source would be resolvable (env/exe-relative/cwd). The user's
// explicit choice is never silently overridden by a fallback.
func TestInstallExplicitSourceIsFinal(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")
	t.Setenv("SEMANTIX_SKILL_DIR", src) // valid fallback exists

	code, _, stderr := installRuns(t,
		"--target", "claude-code", "--dir", dest,
		"--source", filepath.Join(t.TempDir(), "bogus"))
	if code != 1 {
		t.Fatalf("code = %d, want 1 (stderr %q)", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("install must not proceed when --source is invalid")
	}
}

// TestInstallJSONFailureEnvelope: with --json a runtime failure still emits
// the §4.2 envelope (ok=false, error:{code,message}) instead of silent stdout.
func TestInstallJSONFailureEnvelope(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dest")
	code, stdout, _ := installRuns(t,
		"--target", "custom", "--dir", dest,
		"--source", filepath.Join(t.TempDir(), "nope"), "--json")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	var env installEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not a JSON envelope: %v\n%s", err, stdout)
	}
	if env.OK || env.Command != "install" || env.Error == nil {
		t.Fatalf("envelope = %+v, want ok=false with error payload", env)
	}
	if errEnv, ok := env.Error.(map[string]any); !ok || errEnv["code"] != float64(1) {
		t.Fatalf("error payload = %v, want code 1", env.Error)
	}
}

// TestInstallUninstallRejectsTraversal: manifest entries that escape dest
// (.., absolute paths) are skipped, never resolved; files outside dest stay.
func TestInstallUninstallRejectsTraversal(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	dest := filepath.Join(t.TempDir(), "dest")
	if code, _, stderr := installRuns(t, "--target", "custom", "--dir", dest, "--source", src); code != 0 {
		t.Fatalf("install code = %d, stderr = %q", code, stderr)
	}
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := installManifest{
		Command: "install",
		Target:  "custom",
		Version: cliVersion,
		Files:   []string{"SKILL.md", "../victim.txt", filepath.Join(t.TempDir(), "abs.txt")},
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, installManifestName), content, 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := installRuns(t, "--target", "custom", "--dir", dest, "--uninstall")
	if code != 0 {
		t.Fatalf("uninstall code = %d, stderr = %q", code, stderr)
	}
	if data, err := os.ReadFile(victim); err != nil || string(data) != "must survive" {
		t.Fatalf("victim file was touched: %q, err=%v", data, err)
	}
	if !strings.Contains(stdout, "skipped unsafe manifest entry") {
		t.Fatalf("stdout must report skipped unsafe entries:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("safe manifest entry not removed")
	}
}

// TestInstallUninstallCorruptManifest: a corrupt manifest is a runtime error
// (exit 1), not a silent success.
func TestInstallUninstallCorruptManifest(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, installManifestName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := installRuns(t, "--target", "custom", "--dir", dest, "--uninstall")
	if code != 1 {
		t.Fatalf("code = %d, want 1 (stderr %q)", code, stderr)
	}
}

// TestInstallRefusesSourceOverlap: installing into the payload source itself
// (or a subdirectory of it) is refused.
func TestInstallRefusesSourceOverlap(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent-skill")
	writeSkillPayload(t, src)
	for _, dest := range []string{src, filepath.Join(src, "sub")} {
		code, _, stderr := installRuns(t,
			"--target", "custom", "--dir", dest, "--source", src)
		if code != 1 {
			t.Errorf("dest=%s: code = %d, want 1 (stderr %q)", dest, code, stderr)
		}
	}
}

// readInstallManifest decodes the manifest written into dest.
func readInstallManifest(t *testing.T, dest string) installManifest {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dest, installManifestName))
	if err != nil {
		t.Fatalf("manifest not found: %v", err)
	}
	var m installManifest
	if err := json.Unmarshal(content, &m); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	return m
}
