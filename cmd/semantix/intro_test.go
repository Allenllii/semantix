package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIntroRendersStaticWordmarkForNonTerminalOutput(t *testing.T) {
	var out bytes.Buffer
	if code := runIntro(nil, &out); code != 0 {
		t.Fatalf("runIntro() code = %d, want 0", code)
	}
	text := out.String()
	if !strings.Contains(text, "Semantix v0.2.0") {
		t.Fatalf("intro missing version: %q", text)
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("non-terminal output contains ANSI escapes: %q", text)
	}
	if lines := strings.Count(text, "\n"); lines != 11 {
		t.Fatalf("intro newline count = %d, want 11", lines)
	}
}

func TestRunDispatchesIntro(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"intro", "--no-animation"}, &stdout, &stderr, dependencies{}); code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Semantix v0.2.0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestAnimationUsesDisposableAlternateScreen(t *testing.T) {
	var out bytes.Buffer
	frames := []float64{0.08, 1}
	renderIntroAnimation(&out, frames, false, wideIntroLayout, 0)
	text := out.String()
	if !strings.Contains(text, "\x1b[?1049h") || !strings.Contains(text, "\x1b[?1049l") {
		t.Fatalf("animation does not enter and leave alternate screen: %q", text)
	}
	if !strings.HasSuffix(text, renderIntroFrame(1, false)) {
		t.Fatalf("normal scrollback does not end with one final frame: %q", text)
	}
}

func TestIntroAdaptsContinuouslyToTerminalSize(t *testing.T) {
	layout := introLayoutForSize(68, 19)
	if got := introWidth(layout); got > 66 {
		t.Fatalf("narrow intro width = %d, want <= 66", got)
	}
	if layout.block != narrowIntroLayout.block || layout.letterGap != narrowIntroLayout.letterGap {
		t.Fatalf("68-column terminal selected layout %#v, want narrow layout %#v", layout, narrowIntroLayout)
	}
	if layout := introLayoutForSize(100, 20); utf8.RuneCountInString(layout.block) != 2 || len(layout.letterGap) != 1 {
		t.Fatalf("100-column terminal selected layout %#v, want double blocks with 1-column gaps", layout)
	}
	if layout := introLayoutForSize(120, 20); len(layout.letterGap) != 4 {
		t.Fatalf("120-column terminal selected gap %q, want 4 spaces", layout.letterGap)
	}
	if layout := introLayoutForSize(160, 20); layout.letterGap != wideIntroLayout.letterGap {
		t.Fatalf("160-column terminal selected gap %q, want wide gap %q", layout.letterGap, wideIntroLayout.letterGap)
	}
	if layout := introLayoutForSize(40, 20); layout.block != "" {
		t.Fatalf("40-column terminal selected block %q, want plain fallback", layout.block)
	}
	if layout := introLayoutForSize(100, 10); layout.block != "" {
		t.Fatalf("10-row terminal selected block %q, want plain fallback", layout.block)
	}
}

func TestAnimationSweepsFromTopLeftToBottomRight(t *testing.T) {
	// 36 visible pixels completes S and reveals only the first pixel of E.
	frame := renderIntroFrameWithLayout(36.0/280, false, narrowIntroLayout)
	lines := strings.Split(frame, "\n")
	wordmark := lines[4:11]

	if !strings.Contains(wordmark[6], "█") {
		t.Fatal("S did not finish before the sweep moved to E")
	}
	if !strings.Contains(wordmark[0], "█  █") {
		t.Fatalf("sweep did not reach the top-left of E: %q", wordmark[0])
	}
	if blocks := strings.Count(wordmark[1], "█"); blocks != 1 {
		t.Fatalf("E advanced downward before its top-left pixel: %q", wordmark[1])
	}
}

func TestFinalFrameHasNoDarkScanTail(t *testing.T) {
	frame := renderIntroFrameWithLayout(1, true, narrowIntroLayout)
	if strings.Contains(frame, shadowGreen) {
		t.Fatal("final frame retained the dark scan tail")
	}
}

func TestStarPulseReturnsToBrandGreen(t *testing.T) {
	if got := introStarColor(0.11); got == semantixGreen {
		t.Fatal("star did not brighten during its opening pulse")
	}
	if got := introStarColor(1); got != semantixGreen {
		t.Fatalf("final star color = %q, want brand green %q", got, semantixGreen)
	}
}
