package intro

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRendersStaticWordmarkForNonTerminalOutput(t *testing.T) {
	var out bytes.Buffer
	if code := Run(&out, Options{Version: "v9.9.9"}); code != 0 {
		t.Fatalf("Run() code = %d, want 0", code)
	}
	text := out.String()
	if !strings.Contains(text, "Semantix v9.9.9") {
		t.Fatalf("intro missing version: %q", text)
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("non-terminal output contains ANSI escapes: %q", text)
	}
	if lines := strings.Count(text, "\n"); lines != 11 {
		t.Fatalf("intro newline count = %d, want 11", lines)
	}
}

func TestVersionFallsBackToDev(t *testing.T) {
	var out bytes.Buffer
	if code := Run(&out, Options{}); code != 0 {
		t.Fatalf("Run() code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "Semantix dev") {
		t.Fatalf("empty version did not fall back to dev: %q", out.String())
	}
}

func TestBrandColorMatchesH4Green(t *testing.T) {
	// #009C6D is the finalized H4 brand green; the animation must not drift.
	if brandGreen != "\x1b[38;2;0;156;109m" {
		t.Fatalf("brandGreen = %q, want 24-bit #009C6D", brandGreen)
	}
}

func TestTerminalSupportsEffectsRejectsNonFileWriters(t *testing.T) {
	if TerminalSupportsEffects(&bytes.Buffer{}) {
		t.Fatal("bytes.Buffer misdetected as an interactive terminal")
	}
}

func TestAnimationUsesDisposableAlternateScreen(t *testing.T) {
	var out bytes.Buffer
	frames := []float64{0.08, 1}
	renderAnimation(&out, frames, false, wideLayout, "dev", 0)
	text := out.String()
	if !strings.Contains(text, "\x1b[?1049h") || !strings.Contains(text, "\x1b[?1049l") {
		t.Fatalf("animation does not enter and leave alternate screen: %q", text)
	}
	if !strings.HasSuffix(text, renderFrame(1, false, wideLayout, "dev")) {
		t.Fatalf("normal scrollback does not end with one final frame: %q", text)
	}
}

func TestAdaptsContinuouslyToTerminalSize(t *testing.T) {
	lay := layoutForSize(68, 19)
	if got := width(lay); got > 66 {
		t.Fatalf("narrow intro width = %d, want <= 66", got)
	}
	if lay.block != narrowLayout.block || lay.letterGap != narrowLayout.letterGap {
		t.Fatalf("68-column terminal selected layout %#v, want narrow layout %#v", lay, narrowLayout)
	}
	if lay := layoutForSize(100, 20); utf8.RuneCountInString(lay.block) != 2 || len(lay.letterGap) != 1 {
		t.Fatalf("100-column terminal selected layout %#v, want double blocks with 1-column gaps", lay)
	}
	if lay := layoutForSize(120, 20); len(lay.letterGap) != 4 {
		t.Fatalf("120-column terminal selected gap %q, want 4 spaces", lay.letterGap)
	}
	if lay := layoutForSize(160, 20); lay.letterGap != wideLayout.letterGap {
		t.Fatalf("160-column terminal selected gap %q, want wide gap %q", lay.letterGap, wideLayout.letterGap)
	}
	if lay := layoutForSize(40, 20); lay.block != "" {
		t.Fatalf("40-column terminal selected block %q, want plain fallback", lay.block)
	}
	if lay := layoutForSize(100, 10); lay.block != "" {
		t.Fatalf("10-row terminal selected block %q, want plain fallback", lay.block)
	}
}

func TestAnimationSweepsFromTopLeftToBottomRight(t *testing.T) {
	// 36 visible pixels completes S and reveals only the first pixel of E.
	frame := renderFrame(36.0/280, false, narrowLayout, "dev")
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
	frame := renderFrame(1, true, narrowLayout, "dev")
	if strings.Contains(frame, shadowGreen) {
		t.Fatal("final frame retained the dark scan tail")
	}
}

func TestStarPulseReturnsToBrandGreen(t *testing.T) {
	if got := starColor(0.11); got == brandGreen {
		t.Fatal("star did not brighten during its opening pulse")
	}
	if got := starColor(1); got != brandGreen {
		t.Fatalf("final star color = %q, want brand green %q", got, brandGreen)
	}
}
