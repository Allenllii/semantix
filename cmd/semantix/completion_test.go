package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// completionScript runs `semantix completion <shell>` and returns the
// generated script.
func completionScript(t *testing.T, shell string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"completion", shell}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("completion %s: code = %d, stderr = %q", shell, code, stderr.String())
	}
	return stdout.String()
}

// TestCompletionScriptsCoverTree: every generated script must offer every
// registered command, every command's completion flags, and the closed-set
// flag values (Issue #117 acceptance: all subcommands + main flags).
func TestCompletionScriptsCoverTree(t *testing.T) {
	names := commandNameList()
	values := map[string]bool{
		"session": true, "project": true, "user": true,
		"bm25": true, "vector": true, "hybrid": true,
		"hash": true, "model": true,
		"openai": true, "anthropic": true,
		"yes": true, "no": true, "error": true,
	}
	for _, shell := range completionShellNames {
		script := completionScript(t, shell)
		for _, name := range names {
			if !strings.Contains(script, name) {
				t.Errorf("%s script missing command %q", shell, name)
			}
		}
		for _, c := range commands {
			for _, f := range commandFlagList(c) {
				// fish registers long options as `-l name`, the others
				// embed the full `--flag` tokens.
				needle := f
				if shell == "fish" {
					needle = "-l " + strings.TrimPrefix(f, "--")
				}
				if !strings.Contains(script, needle) {
					t.Errorf("%s script missing flag %s of %s", shell, f, c.name)
				}
			}
		}
		for v := range values {
			if !strings.Contains(script, v) {
				t.Errorf("%s script missing value %q", shell, v)
			}
		}
	}
}

// TestCompletionScriptsDeterministic: the same tree always generates the
// same script bytes, so users can diff upgrades.
func TestCompletionScriptsDeterministic(t *testing.T) {
	for _, shell := range completionShellNames {
		if a, b := completionScript(t, shell), completionScript(t, shell); a != b {
			t.Errorf("%s script is not deterministic", shell)
		}
	}
}

// TestCompletionUsageContract: exit codes follow the U19 contract — 0 for
// --help, 2 for missing/unknown shell or extra positional args.
func TestCompletionUsageContract(t *testing.T) {
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"completion"}, 2},              // missing shell
		{[]string{"completion", "tcsh"}, 2},      // unsupported shell
		{[]string{"completion", "bash", "x"}, 2}, // extra positional
		{[]string{"completion", "--help"}, 0},
		{[]string{"completion", "bash"}, 0},
		{[]string{"completion", "zsh"}, 0},
		{[]string{"completion", "fish"}, 0},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		code := run(tc.args, &stdout, &stderr, productionDependencies())
		if code != tc.want {
			t.Errorf("run(%q) code = %d, want %d (stderr %q)", tc.args, code, tc.want, stderr.String())
		}
	}
}

// TestCompletionFlagsMatchCommandHelp locks completionFlags against each
// command's real FlagSet: a flag listed for completion must appear in
// `semantix <command> --help`. Adding a flag without registering it for
// completion is fine; declaring one that does not exist fails here.
func TestCompletionFlagsMatchCommandHelp(t *testing.T) {
	for _, c := range commands {
		if c.name == "completion" {
			continue // completion takes positional shells, not flags
		}
		var stdout, stderr bytes.Buffer
		if code := run([]string{c.name, "--help"}, &stdout, &stderr, productionDependencies()); code != 0 {
			t.Fatalf("%s --help: code = %d, stderr = %q", c.name, code, stderr.String())
		}
		helpOut := stdout.String() + stderr.String()
		for _, f := range c.completionFlags {
			short := strings.TrimPrefix(f, "--")
			if strings.Contains(helpOut, "-"+short) {
				continue
			}
			// eval/eval-judge/usage/verify keep their FlagSet output on
			// the real os.Stderr (no stderr parameter in their U19
			// signatures); capture it to verify the flag exists.
			realStderr := captureRealStderr(t, func() {
				run([]string{c.name, "--help"}, io.Discard, io.Discard, productionDependencies())
			})
			if !strings.Contains(realStderr, "-"+short) {
				t.Errorf("%s: completion declares %s but --help does not show it", c.name, f)
			}
		}
		for vf := range c.flagValues {
			found := false
			for _, f := range c.completionFlags {
				if f == vf {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: flagValues key %s is not in completionFlags", c.name, vf)
			}
		}
	}
}

// captureRealStderr runs fn with os.Stderr redirected to a pipe and returns
// everything fn wrote to it. Needed because eval/eval-judge/usage/verify
// print --help through the flag package's default os.Stderr output.
func captureRealStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// TestCompletionHelpRenders: completion is a registered product-group
// command in `semantix help`, not a planned placeholder.
func TestCompletionHelpRenders(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("help: code = %d", code)
	}
	if !strings.Contains(stdout.String(), "completion") {
		t.Errorf("help must list completion:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "planned: init config version install completion") {
		t.Errorf("completion must not render as planned:\n%s", stdout.String())
	}
}

// TestCompletionBashScriptExecutes drives the generated bash script in a
// real shell: syntax check plus simulated COMP_WORDS/COMP_CWORD triggers.
func TestCompletionBashScriptExecutes(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	script := completionScript(t, "bash")
	path := filepath.Join(t.TempDir(), "semantix.bash")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}

	cases := []struct {
		words string // COMP_WORDS content
		cword int    // COMP_CWORD
		want  string // expected in COMPREPLY
	}{
		{"semantix ex", 1, "extract"},              // command prefix
		{"semantix extract --sc", 2, "--scope"},    // flag prefix
		{"semantix extract --scope us", 3, "user"}, // closed-set value
		{"semantix search --retriever ve", 3, "vector"},
		{"semantix completion z", 2, "zsh"},  // completion shells
		{"semantix help ex", 2, "extract"},   // help completes commands
		{"semantix unknown --x", 2, ""},      // no crash on unknown command
	}
	for _, tc := range cases {
		cmd := exec.Command(bash, "-c",
			`source "$1"`+"\n"+
				`COMP_WORDS=($2)`+"\n"+
				`COMP_CWORD=$3`+"\n"+
				`_semantix`+"\n"+
				`printf '%s\n' "${COMPREPLY[@]}"`,
			"completion-check", path, tc.words, fmt.Sprint(tc.cword))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bash trigger %q: %v\n%s", tc.words, err, out)
		}
		if tc.want == "" {
			continue // unknown command must not error the function
		}
		if !strings.Contains(string(out), tc.want) {
			t.Errorf("bash completion %q = %q, want contains %q", tc.words, out, tc.want)
		}
	}
}

// TestCompletionZshFishSyntax checks the generated scripts parse in their
// shells when available (CI), skipped elsewhere.
func TestCompletionZshFishSyntax(t *testing.T) {
	checks := []struct {
		shell string
		flag  string
	}{
		{"zsh", "-n"},
		{"fish", "-n"},
	}
	for _, tc := range checks {
		bin, err := exec.LookPath(tc.shell)
		if err != nil {
			t.Skipf("%s not available", tc.shell)
		}
		script := completionScript(t, tc.shell)
		path := filepath.Join(t.TempDir(), "semantix."+tc.shell)
		if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(bin, tc.flag, path).CombinedOutput(); err != nil {
			t.Fatalf("%s -n: %v\n%s", tc.shell, err, out)
		}
	}
}
