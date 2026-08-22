// Package intro renders the branded SEMANTIX pixel-wordmark animation.
// It is shared by the `semantix intro` command and the semantix-agent
// interactive startup (Issue #307, the integration step promised in PR #180).
package intro

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	brandGreen  = "\x1b[38;2;0;156;109m" // H4 brand green #009C6D
	shadowGreen = "\x1b[38;2;0;78;54m"   // half-luminance scan tail
	resetColor  = "\x1b[0m"
)

// Options selects what the intro renders; zero value plays the full
// animation with the "dev" version line.
type Options struct {
	// Version is the product version shown in the banner (ldflags
	// main.version of the calling binary). Empty falls back to "dev".
	Version string
	// NoAnimation renders only the final frame.
	NoAnimation bool
}

var glyphs = map[rune][]string{
	'S': {"11111", "10000", "10000", "11111", "00001", "00001", "11111"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
	'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10101", "10011", "10001", "10001"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'I': {"11111", "00100", "00100", "00100", "00100", "00100", "11111"},
	'X': {"10001", "10001", "01010", "00100", "01010", "10001", "10001"},
}

type layout struct {
	block     string
	letterGap string
}

var (
	wideLayout   = layout{block: "██", letterGap: "     "}
	narrowLayout = layout{block: "█", letterGap: "  "}
	plainLayout  = layout{}
)

// Run renders the intro to stdout following the terminal-capability rules
// (TTY, TERM, NO_COLOR, window size) and returns a process exit code.
func Run(stdout io.Writer, opts Options) int {
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	effects := TerminalSupportsEffects(stdout)
	color := effects && os.Getenv("NO_COLOR") == ""
	columns, rows := terminalSize(stdout)
	lay := layoutForSize(columns, rows)
	if opts.NoAnimation || !effects || lay.block == "" {
		fmt.Fprint(stdout, renderFrame(1, color, lay, version))
		return 0
	}

	const frameCount = 56
	frames := make([]float64, frameCount)
	for frame := range frames {
		frames[frame] = float64(frame+1) / frameCount
	}
	renderAnimation(stdout, frames, color, lay, version, 28*time.Millisecond)
	return 0
}

// TerminalSupportsEffects reports whether w is an interactive terminal that
// can host the animation (character device, TERM not "dumb").
func TerminalSupportsEffects(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb"
}

func renderAnimation(stdout io.Writer, frames []float64, color bool, lay layout, version string, delay time.Duration) {
	// Animate in the terminal's alternate screen buffer. Some terminals do not
	// implement cursor save/restore consistently, especially after a long line
	// wraps. The alternate buffer guarantees that every intermediate frame is
	// discarded at once, then only the final wordmark enters normal scrollback.
	fmt.Fprint(stdout, "\x1b[?1049h\x1b[?25l")
	for _, progress := range frames {
		fmt.Fprint(stdout, "\x1b[2J\x1b[H")
		fmt.Fprint(stdout, renderFrame(progress, color, lay, version))
		time.Sleep(delay)
	}
	fmt.Fprint(stdout, "\x1b[?25h\x1b[?1049l")
	fmt.Fprint(stdout, renderFrame(1, color, lay, version))
}

func layoutForSize(columns, rows int) layout {
	const (
		safeMargin     = 2
		minimumRows    = 13
		letters        = len("SEMANTIX")
		letterColumns  = letters * 5
		letterGaps     = letters - 1
		maximumSpacing = 6
	)
	if rows > 0 && rows < minimumRows {
		return plainLayout
	}
	if columns <= 0 {
		return wideLayout
	}
	available := columns - safeMargin
	for blockWidth := 2; blockWidth >= 1; blockWidth-- {
		remaining := available - letterColumns*blockWidth
		if remaining < letterGaps {
			continue
		}
		spacing := max(1, min(maximumSpacing, remaining/letterGaps)-1)
		return layout{
			block:     strings.Repeat("█", blockWidth),
			letterGap: strings.Repeat(" ", spacing),
		}
	}
	return plainLayout
}

func width(lay layout) int {
	const letters = len("SEMANTIX")
	return letters*5*utf8.RuneCountInString(lay.block) + (letters-1)*utf8.RuneCountInString(lay.letterGap)
}

func renderFrame(progress float64, color bool, lay layout, version string) string {
	const word = "SEMANTIX"
	totalPixels := len(word) * 35
	visiblePixels := int(progress * float64(totalPixels))
	var out strings.Builder

	if color {
		out.WriteString(brandGreen)
	}
	out.WriteString("╭──────────────────────────────╮\n")
	out.WriteString("│ ")
	if color {
		out.WriteString(starColor(progress))
	}
	out.WriteString("✦")
	if color {
		out.WriteString(brandGreen)
	}
	out.WriteString("  Semantix ")
	out.WriteString(version)
	out.WriteString(strings.Repeat(" ", max(1, 17-len(version))))
	out.WriteString("│\n")
	out.WriteString("╰──────────────────────────────╯")
	if color {
		out.WriteString(resetColor)
	}
	out.WriteString("\n\n")
	if lay.block == "" {
		if color {
			out.WriteString(brandGreen)
		}
		out.WriteString("SEMANTIX\n")
		if color {
			out.WriteString(resetColor)
		}
		return out.String()
	}

	for row := 0; row < 7; row++ {
		for letterIndex, letter := range word {
			glyph := glyphs[letter]
			for column := 0; column < 5; column++ {
				// Reveal each glyph from its top-left corner to its
				// bottom-right corner, then continue into the next glyph.
				// This creates the original diagonal sweep across the word.
				pixelIndex := letterIndex*35 + row*5 + column
				on := glyph[row][column] == '1'
				visible := pixelIndex < visiblePixels
				if on && visible {
					if color && progress < 1 && pixelIndex+18 > visiblePixels {
						out.WriteString(shadowGreen)
					} else if color {
						out.WriteString(brandGreen)
					}
					out.WriteString(lay.block)
				} else {
					out.WriteString(strings.Repeat(" ", utf8.RuneCountInString(lay.block)))
				}
			}
			if letterIndex < len(word)-1 {
				out.WriteString(lay.letterGap)
			}
		}
		if color {
			out.WriteString(resetColor)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func starColor(progress float64) string {
	// Two restrained pulses: one as the scan begins and one as it settles.
	const pulseWidth = 0.22
	strength := 0.0
	if progress < pulseWidth {
		strength = math.Sin(math.Pi * progress / pulseWidth)
	} else if progress > 1-pulseWidth && progress < 1 {
		strength = math.Sin(math.Pi * (progress - (1 - pulseWidth)) / pulseWidth)
	}
	base := [3]float64{0, 156, 109}
	highlight := [3]float64{105, 255, 211}
	r := int(base[0] + (highlight[0]-base[0])*strength)
	g := int(base[1] + (highlight[1]-base[1])*strength)
	b := int(base[2] + (highlight[2]-base[2])*strength)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}
