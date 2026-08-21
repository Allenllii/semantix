package slice

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	pathpkg "path"
	"sort"
	"strconv"
	"strings"
	"time"

	kernelevent "semantix/kernel/event"
)

// Extractor turns session transcripts into slices.
type Extractor interface {
	// Extract processes one session transcript (JSONL lines) and returns new
	// or updated slices ready for store.Put.
	Extract(sessionJSONL []byte, meta SliceMeta) ([]*Slice, error)
}

// transcriptLine is one line of a session JSONL transcript (Reasonix-style).
// Only the fields the extractor needs are decoded; unknown fields are ignored.
type transcriptLine struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	} `json:"tool_calls,omitempty"`
}

// Limits for extracted slice content (kept modest for the MVP).
const (
	maxPromptLen  = 4000 // bytes of a Prompt slice
	maxResultLen  = 8000 // bytes of a Result slice
	minToolSeq    = 2    // minimum tool calls for a ToolPattern slice
	minContextUse = 2    // repeated observations only; one-off paths are noise
	maxContextTop = 5    // entries retained per Context summary section
)

type extractor struct{}

// NewExtractor returns the default extractor: turn-boundary segmentation,
// tool-sequence (T-Slice) extraction and final-result capture.
func NewExtractor() Extractor { return extractor{} }

// Extract parses one session JSONL transcript into slices:
//   - Prompt slice: first user message of each turn (bounded)
//   - ToolPattern slice: tool-name sequence of each turn (>= 2 calls)
//   - Context slice: repeated project paths, directories, and command heads
//   - Result slice: final assistant answer of the session (bounded)
//
// Malformed lines are skipped (tolerant). Duplicate content (same ID) is
// merged. Slice IDs are content hashes (deterministic, dedup-friendly); a
// stable UUID strategy can replace this in M1 (see Slice.ID doc).
func (extractor) Extract(sessionJSONL []byte, meta SliceMeta) ([]*Slice, error) {
	lines, perr := parseTranscript(sessionJSONL)
	var out []*Slice
	var turn []transcriptLine
	flushTurn := func() {
		if s := promptSlice(turn, meta); s != nil {
			out = append(out, s)
		}
		if s := toolPatternSlice(turn, meta); s != nil {
			out = append(out, s)
		}
		turn = nil
	}
	for _, l := range lines {
		if l.Role == "user" && len(turn) > 0 {
			flushTurn()
		}
		turn = append(turn, l)
	}
	if len(turn) > 0 {
		flushTurn()
	}
	if s := contextSlice(lines, meta); s != nil {
		out = append(out, s)
	}
	if s := finalResultSlice(lines, meta); s != nil {
		out = append(out, s)
	}
	// Partial results are still returned; perr reports a truncated scan
	// (oversized line) so callers can decide whether to retry/scale up.
	return dedup(out), perr
}

var contextPathKeys = map[string]bool{
	"path": true, "paths": true, "file": true, "files": true,
	"file_path": true, "file_paths": true, "source_path": true, "destination_path": true,
	"directory": true, "directories": true, "dir": true, "dirs": true,
	"cwd": true, "workdir": true,
}

var shellToolNames = map[string]bool{
	"bash": true, "shell": true, "exec_command": true, "run_command": true,
}

var commandFamilies = map[string]bool{
	"cargo": true, "docker": true, "git": true, "go": true, "make": true,
	"npm": true, "pnpm": true, "python": true, "python3": true, "uv": true, "yarn": true,
}

type contextEntry struct {
	value string
	count int
}

func contextSlice(lines []transcriptLine, meta SliceMeta) *Slice {
	paths := map[string]int{}
	dirs := map[string]int{}
	commands := map[string]int{}
	for _, line := range lines {
		for _, call := range line.ToolCalls {
			args := decodeToolArgs(call.Arguments)
			for _, raw := range contextPaths(args) {
				p, ok := normalizeContextPath(raw)
				if !ok {
					continue
				}
				paths[p]++
				if dir := pathpkg.Dir(p); dir != "." {
					dirs[dir]++
				}
			}
			if shellToolNames[strings.ToLower(call.Name)] {
				if head := commandHead(commandValue(args)); head != "" {
					commands[head]++
				}
			}
		}
	}

	pathTop := topContextEntries(paths)
	dirTop := topContextEntries(dirs)
	commandTop := topContextEntries(commands)
	if len(pathTop)+len(dirTop)+len(commandTop) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("Project context observed from repeated tool calls:\n")
	writeContextSection(&b, "Frequent paths:", pathTop)
	writeContextSection(&b, "Frequent directories:", dirTop)
	writeContextSection(&b, "Common command heads:", commandTop)
	return newSlice(Context, Project, []byte(strings.TrimSpace(b.String())), meta)
}

func decodeToolArgs(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	if encoded, ok := value.(string); ok {
		if json.Unmarshal([]byte(encoded), &value) != nil {
			return nil
		}
	}
	return value
}

func contextPaths(value any) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	for key, field := range object {
		if contextPathKeys[strings.ToLower(key)] {
			out = append(out, stringValues(field)...)
		}
		if nested, ok := field.(map[string]any); ok {
			out = append(out, contextPaths(nested)...)
		}
		if list, ok := field.([]any); ok {
			for _, item := range list {
				out = append(out, contextPaths(item)...)
			}
		}
	}
	return out
}

func stringValues(value any) []string {
	switch value := value.(type) {
	case string:
		return []string{value}
	case []any:
		var out []string
		for _, item := range value {
			out = append(out, stringValues(item)...)
		}
		return out
	default:
		return nil
	}
}

func normalizeContextPath(raw string) (string, bool) {
	p := strings.TrimSpace(strings.Trim(raw, `"'`))
	p = strings.ReplaceAll(p, `\`, "/")
	if p == "" || p == "." || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") ||
		strings.Contains(p, "://") || (len(p) >= 2 && p[1] == ':') {
		return "", false
	}
	p = pathpkg.Clean(p)
	p = strings.TrimPrefix(p, "./")
	if p == "" || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}

func commandValue(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	command, _ := object["command"].(string)
	return command
}

func commandHead(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	first := strings.Trim(fields[0], `"'`)
	if !validCommandToken(first) {
		return ""
	}
	head := first
	if commandFamilies[strings.ToLower(first)] && len(fields) > 1 {
		second := strings.Trim(fields[1], `"'`)
		if !strings.HasPrefix(second, "-") && validCommandToken(second) {
			head += " " + second
		}
	}
	return head
}

func validCommandToken(token string) bool {
	if token == "" || strings.ContainsAny(token, `/\\=:$`) {
		return false
	}
	for _, r := range token {
		if !(r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func topContextEntries(counts map[string]int) []contextEntry {
	entries := make([]contextEntry, 0, len(counts))
	for value, count := range counts {
		if count >= minContextUse {
			entries = append(entries, contextEntry{value: value, count: count})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].value < entries[j].value
	})
	if len(entries) > maxContextTop {
		entries = entries[:maxContextTop]
	}
	return entries
}

func writeContextSection(b *strings.Builder, title string, entries []contextEntry) {
	if len(entries) == 0 {
		return
	}
	b.WriteString(title)
	b.WriteByte('\n')
	for _, entry := range entries {
		b.WriteString("- ")
		b.WriteString(entry.value)
		b.WriteString(" (")
		b.WriteString(strconv.Itoa(entry.count))
		b.WriteString(")\n")
	}
}

func parseTranscript(data []byte) ([]transcriptLine, error) {
	var out []transcriptLine
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var tl transcriptLine
		if err := json.Unmarshal(line, &tl); err != nil {
			continue // tolerant: skip malformed lines
		}
		if tl.Role == "" {
			if e, err := kernelevent.FromJSON(line); err == nil {
				switch e.Kind {
				case kernelevent.SliceHit:
					tl = transcriptLine{Role: "user", Content: "slice hit " + string(e.Data)}
				case kernelevent.SliceInject:
					tl = transcriptLine{Role: "user", Content: "slice inject " + string(e.Data)}
				case kernelevent.PrefetchHit:
					tl = transcriptLine{Role: "user", Content: "prefetch hit targets " + string(e.Data)}
				case kernelevent.PrefetchWaste:
					tl = transcriptLine{Role: "user", Content: "prefetch waste targets " + string(e.Data)}
				case kernelevent.EvolutionTick:
					tl = transcriptLine{Role: "user", Content: "evolution tick params " + string(e.Data)}
				}
			}
		}
		if tl.Role == "" {
			continue
		}
		out = append(out, tl)
	}
	return out, sc.Err()
}

func promptSlice(turn []transcriptLine, meta SliceMeta) *Slice {
	for _, l := range turn {
		if l.Role == "user" {
			content := compressText(l.Content, maxPromptLen)
			if len(content) == 0 {
				return nil
			}
			return newSlice(Prompt, Project, content, compressionMeta(meta, l.Content, content))
		}
	}
	return nil
}

func toolPatternSlice(turn []transcriptLine, meta SliceMeta) *Slice {
	var names []string
	for _, l := range turn {
		for _, tc := range l.ToolCalls {
			if tc.Name != "" {
				names = append(names, tc.Name)
			}
		}
	}
	if len(names) < minToolSeq {
		return nil
	}
	// Space-joined (not \x1f): keeps the sequence tokenizable by the BM25
	// whitespace/CJK splitter (kernel/bm25). Tool-name tokens are plain words.
	return newSlice(ToolPattern, Project, []byte(strings.Join(names, " ")), meta)
}

func finalResultSlice(lines []transcriptLine, meta SliceMeta) *Slice {
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if l.Role == "assistant" && len(l.ToolCalls) == 0 {
			content := compressText(l.Content, maxResultLen)
			if len(content) == 0 {
				return nil
			}
			return newSlice(Result, Project, content, compressionMeta(meta, l.Content, content))
		}
	}
	return nil
}

func compressionMeta(meta SliceMeta, original string, stored []byte) SliceMeta {
	meta.CompressionVersion = compressionVersion
	meta.OriginalBytes = len([]byte(original))
	meta.StoredBytes = len(stored)
	return meta
}

func newSlice(t SliceType, sc Scope, content []byte, meta SliceMeta) *Slice {
	return &Slice{
		ID:        sliceID(content, t, sc),
		Type:      t,
		Scope:     sc,
		Content:   content,
		Weight:    1.0,
		Meta:      meta,
		CreatedAt: time.Now().Unix(),
	}
}

// sliceID is a deterministic content-derived ID. Type and scope are mixed in
// so identical content of different kinds cannot collide.
func sliceID(content []byte, t SliceType, sc Scope) string {
	h := sha256.New()
	h.Write([]byte{byte(t), byte(sc)})
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func dedup(slices []*Slice) []*Slice {
	seen := make(map[string]bool, len(slices))
	var out []*Slice
	for _, s := range slices {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	return out
}
