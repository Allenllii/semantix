package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func stubInteractiveTerminal(t *testing.T, interactive bool) {
	t.Helper()
	restore := isInteractiveTerminal
	t.Cleanup(func() { isInteractiveTerminal = restore })
	isInteractiveTerminal = func(io.Writer) bool { return interactive }
}

func TestShouldPlayIntroOnBareInteractiveInvocation(t *testing.T) {
	stubInteractiveTerminal(t, true)
	if !shouldPlayIntro(nil, io.Discard) {
		t.Fatal("bare interactive invocation should play the intro")
	}
}

func TestShouldPlayIntroSkipsAnyArgs(t *testing.T) {
	stubInteractiveTerminal(t, true)
	for _, args := range [][]string{{"version"}, {"--version"}, {"run", "task"}} {
		if shouldPlayIntro(args, io.Discard) {
			t.Fatalf("invocation with args %v must not play the intro", args)
		}
	}
}

func TestShouldPlayIntroRespectsEscapeHatch(t *testing.T) {
	stubInteractiveTerminal(t, true)
	t.Setenv("SEMANTIX_NO_INTRO", "1")
	if shouldPlayIntro(nil, io.Discard) {
		t.Fatal("SEMANTIX_NO_INTRO must skip the intro")
	}
}

func TestShouldPlayIntroRequiresInteractiveTerminal(t *testing.T) {
	// No stub: a bytes.Buffer can never be a character device, so piped and
	// redirected startups stay byte-identical to the pre-intro behavior.
	if shouldPlayIntro(nil, &bytes.Buffer{}) {
		t.Fatal("non-terminal stdout must not play the intro")
	}
}

func TestMaybePlayIntroWritesNothingWhenSkipped(t *testing.T) {
	var out bytes.Buffer
	maybePlayIntro([]string{"run"}, &out)
	if out.Len() != 0 {
		t.Fatalf("skipped intro wrote %d bytes, want 0", out.Len())
	}
}

func TestMaybePlayIntroRendersWordmarkWhenInteractive(t *testing.T) {
	stubInteractiveTerminal(t, true)
	var out bytes.Buffer
	maybePlayIntro(nil, &out)
	if !strings.Contains(out.String(), "Semantix dev") {
		t.Fatalf("interactive startup did not render the intro banner: %q", out.String())
	}
}
