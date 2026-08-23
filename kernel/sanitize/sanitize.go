// Package sanitize implements the versioned, deterministic sanitization
// pipeline for untrusted session content (Issue #278, Security-安全设计.md
// §3.1): escape-sequence stripping, prompt-injection feature removal and
// sensitive-pattern redaction. It is a pure, LLM-free function library —
// same input always yields the same output, so sanitized bytes can enter
// the injection-set fingerprint (Security §2.3: determinism ≠ completeness;
// adversarial coverage is locked by tests, known bypasses are documented
// as such).
//
// The pipeline is wired on three sides:
//
//   - write side: kernel/slice extractor sanitizes every ingested slice at
//     creation (SliceMeta.SanitizeVersion records the rule version);
//   - inject side: kernel/inject sanitizes (idempotently) before assembling
//     the injection block, defending legacy unsanitized slices;
//   - read side: the LLM judge reads the full pipeline (never raw history).
package sanitize

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version identifies the rule-set revision. Bump on any rule change
// (feature phrase / redaction pattern): freshly ingested slices record it
// in SliceMeta.SanitizeVersion, while existing slices are covered by the
// idempotent inject-side pass — no library rewrite needed.
const Version = "v1"

// --- escape-sequence stripping (migrated from kernel/judge.Sanitize) ---

// EscapeSequences strips ANSI/OSC/DCS/PM/APC/SOS escape sequences from
// untrusted content (Issue #8 acceptance ④): a cached answer or session
// line may carry terminal-control payloads, and both the terminal output
// path and the sanitization pipeline must read a deterministically cleaned
// copy. C1 control bytes are only recognized as their own runes (never
// inside multi-byte UTF-8), every escape family is consumed to its
// terminator, and unterminated sequences strip only the start byte —
// legal text always survives intact.
func EscapeSequences(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			switch c := s[0]; {
			case c == 0x1b:
				if len(s) < 2 {
					return b.String()
				}
				s = consumeESCSeq(s)
			case c == 0x90 || c == 0x98 || c == 0x9b || c == 0x9c || c == 0x9d || c == 0x9e || c == 0x9f:
				s = consumeC1Seq(s)
			default:
				b.WriteByte(c)
				s = s[1:]
			}
			continue
		}
		switch r {
		case 0x1b:
			if len(s) < 2 {
				return b.String()
			}
			s = consumeESCSeq(s)
		case 0x90, 0x98, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f:
			// A multi-byte-encoded C1 escape (e.g. 0xC2 0x9B → U+009B). It must
			// still be stripped — a terminal may act on it — but dispatch on the
			// DECODED value and scan the bytes AFTER the encoding (s[size:]). The
			// old code passed the un-advanced s to consumeC1Seq, whose switch saw
			// the UTF-8 lead byte 0xC2, always fell to the OSC/DCS (to-ST) branch,
			// and over-consumed unrelated text up to a distant BEL/ST (Issue #358).
			rest := s[size:]
			switch r {
			case 0x9b: // CSI: consume up to its own final byte
				if out, ok := consumeToFinal(rest); ok {
					s = out
				} else {
					s = rest
				}
			case 0x9c: // lone ST: nothing follows to consume
				s = rest
			default: // DCS/OSC/PM/APC/SOS: consume up to ST
				if out, ok := consumeToST(rest); ok {
					s = out
				} else {
					s = rest
				}
			}
		default:
			b.WriteString(s[:size])
			s = s[size:]
		}
	}
	return b.String()
}

// consumeESCSeq consumes an ESC-prefixed sequence (s starts with 0x1b).
func consumeESCSeq(s string) string {
	rest, ok := consumeESCBody(s)
	if ok {
		return rest
	}
	// Unterminated: strip only the start byte, keep the rest (legal text
	// survives intact).
	return s[1:]
}

// consumeC1Seq consumes a C1-prefixed sequence (s starts with the raw C1
// byte). C1 escapes never have the two-char ESC form; dispatch by byte.
func consumeC1Seq(s string) string {
	switch s[0] {
	case 0x9b: // CSI
		if out, ok := consumeToFinal(s[1:]); ok {
			return out
		}
		return s[1:]
	case 0x9c: // lone ST
		return s[1:]
	default: // DCS/OSC/PM/APC/SOS: consume up to ST
		if out, ok := consumeToST(s[1:]); ok {
			return out
		}
		return s[1:]
	}
}

// consumeESCBody consumes the body of an ESC-prefixed sequence.
func consumeESCBody(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	switch s[1] {
	case '[': // CSI: consume up to the final byte @A–Z[\]^_`a–z{|}~
		return consumeToFinal(s[2:])
	case ']', 'P', '^', '_', 'X': // OSC/DCS/PM/APC/SOS: consume up to ST (BEL / 0x9C / ESC \)
		return consumeToST(s[2:])
	case '\\': // lone ST in ESC form
		return s[2:], true
	default:
		if s[1] < 0x80 {
			return s[2:], true // two-character sequence
		}
		return "", false // ESC before multi-byte char: strip only ESC
	}
}

// consumeToFinal consumes bytes up to and including a CSI final byte
// (@A–Z[\]^_`a–z{|}~). Returns ok=false when no final byte is found.
func consumeToFinal(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x40 && c <= 0x7e {
			return s[i+1:], true
		}
	}
	return "", false
}

// consumeToST consumes bytes up to and including the OSC/DCS terminator
// (BEL 0x07, C1 ST 0x9c, or the two-byte ESC \ form).
func consumeToST(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x07 || c == 0x9c {
			return s[i+1:], true
		}
		if c == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
			return s[i+2:], true
		}
	}
	return "", false
}

// --- prompt-injection feature stripping (Issue #278) ---

// featurePhrases are high-confidence injection payload phrases (English and
// Chinese) stripped verbatim from untrusted content (Security §3.1: 指令
// 前缀/角色扮演/命令替换). Matching is rune-level case-folding with word
// boundaries, so "ignore previous instructions" inside a larger identifier
// is left alone while payload phrases are removed deterministically. The
// table is deliberately conservative: low-confidence guidance words
// ("act as", "你现在是") are NOT included — false stripping of legitimate
// discussion would corrupt user-visible content (v1 trade-off, spec §2.2).
var featurePhrases = []string{
	// Instruction override families (high confidence: near-unambiguous
	// injection intent).
	"ignore previous instructions",
	"ignore all previous instructions",
	"ignore all instructions",
	"disregard previous instructions",
	"disregard all instructions",
	"forget previous instructions",
	"forget all previous instructions",
	"override all previous instructions",
	// System-prompt impersonation.
	"your new system prompt",
	"your new instructions",
	// Role-play takeover (high-confidence full forms only).
	"from now on you are",
	"pretend you are",
	// Chinese payload forms.
	"忽略之前的指令",
	"忽略以上所有内容",
	"忽略以上内容",
	"无视之前的指令",
	"忘记之前的指令",
	"你的新系统提示",
	"你的新指令",
}

// wordBoundary reports whether rune c can sit next to a stripped phrase
// without making it part of a larger word: letters/digits/underscore are
// word characters (no boundary); anything else is a boundary. CJK runes
// are always boundaries — CJK has no word separation, but payload phrases
// are multi-rune and specific enough that adjacent CJK context is safe
// (and CJK is classified as a letter by unicode.IsLetter, which would
// otherwise block matching inside normal Chinese sentences).
func wordBoundary(r rune) bool {
	if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
		return true
	}
	return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}

// stripFeatures removes every feature phrase (case-insensitive, word
// boundary guarded). Whitespace is collapsed ONLY when at least one phrase
// was actually removed (payload phrases often carry trailing
// punctuation/spacing) — untouched text keeps its exact layout, so normal
// multi-line content never changes (write-side byte-stability anchor).
func stripFeatures(s string) string {
	folded := foldString(s)
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	rs := []rune(s)
	frs := []rune(folded)
	removed := false
	for i < len(rs) {
		matched := false
		for _, phrase := range featurePhrases {
			needle := []rune(phrase)
			if i+len(needle) > len(rs) {
				continue
			}
			if string(frs[i:i+len(needle)]) != phrase {
				continue
			}
			// Word-boundary guard on both sides: the phrase must not be a
			// substring of a larger word. The guard uses the ORIGINAL runes
			// (folding never changes rune count — see foldString).
			beforeOK := i == 0 || wordBoundary(rs[i-1])
			afterOK := i+len(needle) == len(rs) || wordBoundary(rs[i+len(needle)])
			if !beforeOK || !afterOK {
				continue
			}
			matched = true
			removed = true
			i += len(needle)
			break
		}
		if matched {
			continue // phrase dropped; whitespace collapse handles residue
		}
		b.WriteRune(rs[i])
		i++
	}
	if !removed {
		return s
	}
	return collapseSpace(b.String())
}

// collapseSpace folds runs of whitespace (after phrase removal a payload
// often leaves "  ," or double spaces) into a single space and trims.
func collapseSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	started := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			prevSpace = true
			continue
		}
		if prevSpace && started {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
		started = true
		prevSpace = false
	}
	if !started {
		return ""
	}
	return b.String()
}

// foldString lowercases rune-by-rune (unicode.ToLower maps one rune to one
// rune, so offsets never drift — a byte-level ToLower would misalign for
// fold-special runes like İ/ẞ).
func foldString(s string) string {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return string(rs)
}

// --- sensitive-pattern redaction (Issue #278) ---

// Redaction patterns are deliberately high-confidence forms only: platform
// key prefixes, well-formed emails and anchored home-directory paths.
// Generic path redaction is out of scope (false-positive surface too wide,
// spec §2.3).
var redactionPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Platform keys: OpenAI-style sk-, AWS AKIA, GitHub ghp_, Slack xox*.
	{regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`), "[REDACTED_KEY]"},
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "[REDACTED_KEY]"},
	{regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`), "[REDACTED_KEY]"},
	{regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), "[REDACTED_KEY]"},
	// Email addresses (no line crossing; the \b guards on ASCII letters).
	{regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`), "[REDACTED_EMAIL]"},
	// Home-directory paths (anchored, so project paths like /workspace or
	// /opt stay intact).
	{regexp.MustCompile(`(?:/home|/Users)/[A-Za-z0-9_.\-]+`), "[REDACTED_PATH]"},
	{regexp.MustCompile(`(?i)C:\\Users\\[A-Za-z0-9_.\-]+`), "[REDACTED_PATH]"},
}

// redact replaces every sensitive pattern with its fixed placeholder
// (deterministic: same input, same output).
func redact(s string) string {
	for _, p := range redactionPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// Sanitize is the full pipeline: escape stripping → injection-feature
// stripping → sensitive-pattern redaction. Pure and idempotent:
// Sanitize(Sanitize(s)) == Sanitize(s), so the inject side can re-run it
// over already-sanitized slices without changing the injection block.
func Sanitize(s string) string {
	s = EscapeSequences(s)
	s = stripFeatures(s)
	s = redact(s)
	return s
}
