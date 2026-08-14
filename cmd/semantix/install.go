package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// installTargets are the harnesses `semantix install` supports (Issue #119).
// reasonix is the Gnosil/DeepSeek-Reasonix fork, which already ships a
// built-in integration ([semantix] enabled=true); install lands the
// agent-skill reference docs locally and prints the one-line config step.
// claude-code uses the Claude Code agent-skills directory; custom is any
// directory the user names with --dir.
var installTargets = []string{"reasonix", "claude-code", "custom"}

// installManifestName is the bookkeeping file `semantix install` writes into
// the destination. It lists exactly which files install placed there, so
// --uninstall can remove them without touching anything the user added
// afterwards. It is intentionally a dotfile: it must not shadow the payload.
const installManifestName = ".semantix-install.json"

// installFileAction is one file's outcome on install/uninstall.
type installFileAction string

const (
	actionInstalled installFileAction = "installed"
	actionUpdated   installFileAction = "updated"
	actionUnchanged installFileAction = "unchanged"
	actionRemoved   installFileAction = "removed"
	actionSkipped   installFileAction = "skipped" // listed in manifest but absent (already uninstalled)
)

// installFile is one payload file and what happened to it.
type installFile struct {
	Path   string            `json:"path"`
	Action installFileAction `json:"action"`
}

// installReport is the `data` payload of `semantix install --json` (§4.2).
type installReport struct {
	Target string        `json:"target"`
	Source string        `json:"source,omitempty"`
	Dest   string        `json:"dest"`
	Files  []installFile `json:"files"`
	Next   []string      `json:"next,omitempty"`
}

// installEnvelope is the §4.2 JSON envelope: ok + command + data + error +
// version, mirroring the doctor command's envelope.
type installEnvelope struct {
	OK      bool          `json:"ok"`
	Command string        `json:"command"`
	Data    installReport `json:"data"`
	Error   any           `json:"error"`
	Version string        `json:"version"`
}

// installManifest is the bookkeeping file written to the destination.
type installManifest struct {
	Command string   `json:"command"`
	Target  string   `json:"target"`
	Version string   `json:"version"`
	Files   []string `json:"files"`
}

// runInstall implements the U25 agent-skill installer (Issue #119): it drops
// the agent-skill payload (SKILL.md + tools schema + hooks + config + scripts)
// into the target harness directory. Re-runs are safe (byte-identical files
// are left untouched, changed ones overwritten), and --uninstall removes
// exactly the files install recorded in its manifest.
func runInstall(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := fs.String("target", "", "harness to install into (reasonix|claude-code|custom)")
	dir := fs.String("dir", "", "destination directory (required for custom; default per target)")
	source := fs.String("source", "", "agent-skill source dir (default: SEMANTIX_SKILL_DIR, exe-relative, ./agent-skill)")
	uninstall := fs.Bool("uninstall", false, "remove a previous install instead of installing")
	jsonOutput := fs.Bool("json", false, "write the report as JSON (envelope per cli-v2-architecture §4.2)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "Usage: semantix install --target reasonix|claude-code|custom [--dir <path>] [--uninstall]")
		return 2
	}
	if *target == "" {
		fmt.Fprintln(stderr, "semantix install: missing --target (want reasonix|claude-code|custom)")
		return 2
	}
	if !installTargetValid(*target) {
		fmt.Fprintf(stderr, "semantix install: invalid target %q (want %s)\n",
			*target, strings.Join(installTargets, "|"))
		return 2
	}

	dest, err := installDestDir(*target, *dir)
	if err != nil {
		fmt.Fprintf(stderr, "semantix install: %v\n", err)
		return 2
	}

	if *uninstall {
		report := runInstallUninstall(*target, dest)
		return emitInstallResult(stdout, stderr, "install", "uninstall", report, *jsonOutput)
	}

	src, err := resolveInstallSource(*source)
	if err != nil {
		fmt.Fprintf(stderr, "semantix install: %v\n", err)
		return 1
	}
	report, err := runInstallCopy(src, dest)
	if err != nil {
		fmt.Fprintf(stderr, "semantix install: %v\n", err)
		return 1
	}
	report.Target = *target
	report.Source = src
	report.Dest = dest
	report.Next = installNextSteps(*target, dest)
	if err := writeInstallManifest(dest, *target, report.Files); err != nil {
		fmt.Fprintf(stderr, "semantix install: %v\n", err)
		return 1
	}
	return emitInstallResult(stdout, stderr, "install", "install", report, *jsonOutput)
}

// installTargetValid reports whether target is one of installTargets.
func installTargetValid(target string) bool {
	for _, t := range installTargets {
		if t == target {
			return true
		}
	}
	return false
}

// installDestDir resolves the destination directory: an explicit --dir wins;
// otherwise each target has a conventional default. custom has no default,
// which is a usage error (handled by the caller via the error return).
func installDestDir(target, dirFlag string) (string, error) {
	if dirFlag != "" {
		return dirFlag, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home directory: %w", err)
	}
	switch target {
	case "reasonix":
		return filepath.Join(home, ".semantix", "agent-skill"), nil
	case "claude-code":
		return filepath.Join(home, ".claude", "skills", "semantix"), nil
	case "custom":
		return "", errors.New("--dir is required for --target custom")
	}
	return "", fmt.Errorf("invalid target %q (want %s)", target, strings.Join(installTargets, "|"))
}

// resolveInstallSource finds the agent-skill payload directory, in order:
// --source > SEMANTIX_SKILL_DIR > <exe-dir>/../agent-skill (release bundle
// layout) > ./agent-skill (repo checkout). A candidate is valid only when it
// contains SKILL.md, the payload's anchor file.
func resolveInstallSource(flagSource string) (string, error) {
	candidates := []string{}
	if flagSource != "" {
		candidates = append(candidates, flagSource)
	}
	if v := os.Getenv("SEMANTIX_SKILL_DIR"); v != "" {
		candidates = append(candidates, v)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "..", "agent-skill"),
			filepath.Join(filepath.Dir(exe), "agent-skill"))
	}
	candidates = append(candidates, "agent-skill")
	for _, c := range candidates {
		if ok, _ := validSkillDir(c); ok {
			return c, nil
		}
	}
	return "", fmt.Errorf("agent-skill source not found (looked at: %s); pass --source <dir> or set SEMANTIX_SKILL_DIR",
		strings.Join(candidates, ", "))
}

// validSkillDir reports whether dir exists and contains SKILL.md.
func validSkillDir(dir string) (bool, error) {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

// installNextSteps prints per-target follow-ups. Both SKILL.md and
// hooks/session-bypass.md are part of the installed payload (the issue's
// acceptance requires the output to reference them).
func installNextSteps(target, dest string) []string {
	switch target {
	case "reasonix":
		return []string{
			fmt.Sprintf("set [semantix] enabled=true in the Reasonix config (built-in integration, zero steps; see %s)",
				filepath.Join(dest, "SKILL.md")),
			fmt.Sprintf("session capture reference: %s", filepath.Join(dest, "hooks", "session-bypass.md")),
		}
	case "claude-code":
		return []string{
			fmt.Sprintf("restart Claude Code; the semantix skill will be picked up from %s", dest),
			fmt.Sprintf("register the tool schema: %s", filepath.Join(dest, "tools", "semantix-lookup.md")),
			fmt.Sprintf("usage guide: %s", filepath.Join(dest, "SKILL.md")),
		}
	default: // custom
		return []string{
			fmt.Sprintf("wire the copied tools into your agent: %s", filepath.Join(dest, "tools", "semantix-lookup.md")),
			fmt.Sprintf("usage guide: %s", filepath.Join(dest, "SKILL.md")),
			fmt.Sprintf("session capture reference: %s", filepath.Join(dest, "hooks", "session-bypass.md")),
		}
	}
}

// runInstallCopy copies every file under src into dest, preserving relative
// paths, and reports each file's action. Directory entries are created as
// needed; existing files are byte-compared so re-runs are no-ops (idempotent,
// §验收标准: 重复安装可安全重跑).
func runInstallCopy(src, dest string) (installReport, error) {
	var report installReport
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil // MkdirAll happens per file
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == installManifestName {
			return nil // never copy a manifest that lives in a source dir
		}
		action, err := copyInstallFile(path, filepath.Join(dest, rel))
		if err != nil {
			return err
		}
		report.Files = append(report.Files, installFile{Path: filepath.ToSlash(rel), Action: action})
		return nil
	})
	return report, err
}

// copyInstallFile writes one payload file into destPath: MkdirAll the parent,
// byte-compare against the existing file, and only write when different.
func copyInstallFile(srcPath, destPath string) (installFileAction, error) {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
		return "", err
	}
	existing, err := os.ReadFile(destPath)
	if err == nil {
		if bytes.Equal(existing, content) {
			return actionUnchanged, nil
		}
		return actionUpdated, writeInstallFile(destPath, content)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return actionInstalled, writeInstallFile(destPath, content)
}

// writeInstallFile writes content with 0600 perms (memory-store convention;
// Windows ignores the permission bits).
func writeInstallFile(path string, content []byte) error {
	return os.WriteFile(path, content, 0o600)
}

// writeInstallManifest records the installed file list at dest so --uninstall
// can remove exactly those files. The manifest itself is not listed in it.
func writeInstallManifest(dest, target string, files []installFile) error {
	m := installManifest{
		Command: "install",
		Target:  target,
		Version: cliVersion,
	}
	for _, f := range files {
		m.Files = append(m.Files, f.Path)
	}
	sort.Strings(m.Files)
	content, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	return writeInstallFile(filepath.Join(dest, installManifestName), content)
}

// runInstallUninstall removes a previous install: it reads the manifest
// written by install and deletes exactly the listed files, then the manifest
// itself. A missing manifest means there is nothing tracked — a no-op, so
// repeated --uninstall is safe. Files the user added after installing are
// never touched (the manifest is the only removal list).
func runInstallUninstall(target, dest string) installReport {
	report := installReport{Target: target, Dest: dest}
	manifestPath := filepath.Join(dest, installManifestName)
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		report.Next = []string{
			fmt.Sprintf("no install manifest at %s — nothing tracked to uninstall", manifestPath),
		}
		return report
	}
	var m installManifest
	if err := json.Unmarshal(content, &m); err != nil {
		report.Next = []string{
			fmt.Sprintf("install manifest at %s is corrupt (%v) — remove it manually, then re-run", manifestPath, err),
		}
		return report
	}
	dirs := map[string]bool{}
	for _, f := range m.Files {
		path := filepath.Join(dest, filepath.FromSlash(f))
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				report.Files = append(report.Files, installFile{Path: f, Action: actionSkipped})
				continue
			}
			report.Files = append(report.Files, installFile{Path: f, Action: actionSkipped})
			continue
		}
		if err := os.Remove(path); err != nil {
			report.Files = append(report.Files, installFile{Path: f, Action: actionSkipped})
			continue
		}
		report.Files = append(report.Files, installFile{Path: f, Action: actionRemoved})
		for p := filepath.Dir(path); p != dest && p != filepath.Dir(p); p = filepath.Dir(p) {
			dirs[p] = true
		}
	}
	if err := os.Remove(manifestPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		report.Next = append(report.Next, fmt.Sprintf("could not remove manifest %s: %v", manifestPath, err))
	}
	// Prune now-empty parent directories bottom-up.
	var sorted []string
	for p := range dirs {
		sorted = append(sorted, p)
	}
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, p := range sorted {
		_ = os.Remove(p) // only succeeds when empty
	}
	return report
}

// emitInstallResult renders the report as the §4.2 JSON envelope (--json) or
// as grep-able human output. The JSON envelope always reports command=install
// (--uninstall is a flag of the install command); the human header shows the
// operation. This function is only reached when the operation itself
// succeeded, so it always returns 0.
func emitInstallResult(stdout, stderr io.Writer, command, op string, report installReport, jsonOutput bool) int {
	if jsonOutput {
		env := installEnvelope{
			OK:      true,
			Command: command,
			Data:    report,
			Version: cliVersion,
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(env); err != nil {
			fmt.Fprintf(stderr, "semantix install: %v\n", err)
			return 1
		}
		return 0
	}
	line := "semantix install"
	if op == "uninstall" {
		line = "semantix install --uninstall"
	}
	fmt.Fprintf(stdout, "# %s v%s target=%s\n", line, cliVersion, report.Target)
	if report.Source != "" {
		fmt.Fprintf(stdout, "  source: %s\n", report.Source)
	}
	fmt.Fprintf(stdout, "  dest:   %s\n", report.Dest)
	for _, f := range report.Files {
		fmt.Fprintf(stdout, "[%s] %s\n", f.Action, f.Path)
	}
	for _, n := range report.Next {
		fmt.Fprintf(stdout, "next: %s\n", n)
	}
	return 0
}
