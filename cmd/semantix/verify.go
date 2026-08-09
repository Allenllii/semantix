package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"semantix/kernel/slice"
)

// verify implements offline replay validation for the M0-2 hypothesis:
// "when using a coding agent, repeated intermediate work appears across
// sessions and can be reused". It splits sessions by time (holdout fraction
// reserved as the replay stream), indexes the earlier turns, then replays
// every user turn of the later sessions and prints a TSV evaluation table
// (query → top-1 hit + score) for human relevance marking.
//
// Output columns (tab-separated, first line is a header):
//   session turn score top1_content query
// After marking each row ✅/❌, relevance rate = marked-correct / total.
// Target for M0-Gate: ≥70% of replayed turns find a "previously done similar"
// top-1 hit.

type verifyOptions struct {
	sessions []string // files or directories (scanned for *.jsonl)
	db       string
	project  string
	holdout  float64 // fraction of sessions reserved as replay stream
	scope    slice.Scope
}

type verifyTurn struct {
	Session string
	Turn    int
	Query   string
}

// transcriptLine mirrors kernel/slice's tolerant JSONL parsing (unknown
// fields ignored) so verify stays independent of extractor internals.
type verifyLine struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func collectSessionFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
					files = append(files, filepath.Join(p, e.Name()))
				}
			}
			continue
		}
		files = append(files, p)
	}
	// Time order: a coding agent names session files with timestamps, but
	// mtime is the reliable proxy for "earlier vs later".
	sort.Slice(files, func(i, j int) bool {
		a, _ := os.Stat(files[i])
		b, _ := os.Stat(files[j])
		return a.ModTime().Before(b.ModTime())
	})
	return files, nil
}

// parseTurns extracts user turns (in order) from one session JSONL.
func parseTurns(path string) ([]verifyTurn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var turns []verifyTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var l verifyLine
		if err := json.Unmarshal(line, &l); err != nil {
			continue // tolerant: skip corrupt lines
		}
		if l.Role != "user" {
			continue
		}
		if strings.TrimSpace(l.Content) == "" {
			continue // skip empty steering lines
		}
		n++
		turns = append(turns, verifyTurn{Session: filepath.Base(path), Turn: n, Query: strings.TrimSpace(l.Content)})
	}
	return turns, sc.Err()
}

func runVerify(args []string, stdout io.Writer, deps dependencies) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	var opt verifyOptions
	fs.Var(stringListFlag{&opt.sessions}, "session", "session JSONL file or directory (repeatable)")
	fs.StringVar(&opt.db, "db", "", "database path override (default: .semantix/project.db)")
	fs.StringVar(&opt.project, "project", "", "project slug")
	fs.Float64Var(&opt.holdout, "holdout", 0.3, "fraction of latest sessions reserved as replay stream (0-1)")
	var scopeName string
	fs.StringVar(&scopeName, "scope", "project", "scope: project|user|session")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(opt.sessions) == 0 || fs.NArg() > 0 {
		fmt.Fprintln(stdout, "Usage: semantix verify --session <file|dir> [--session ...] [--holdout 0.3]")
		return 2
	}
	scope, err := parseScope(scopeName)
	if err != nil {
		fmt.Fprintf(stdout, "verify: %v\n", err)
		return 2
	}
	opt.scope = scope
	if opt.holdout < 0 || opt.holdout > 1 {
		fmt.Fprintln(stdout, "--holdout must be in [0,1]")
		return 2
	}
	if opt.db == "" {
		// verify must not pollute the extract/search store with v- prefixed
		// training slices; keep a dedicated replay database.
		opt.db = ".semantix/verify.db"
	}

	files, err := collectSessionFiles(opt.sessions)
	if err != nil {
		fmt.Fprintf(stdout, "verify: %v\n", err)
		return 1
	}
	if len(files) < 2 {
		fmt.Fprintf(stdout, "verify: need at least 2 session files (have %d)\n", len(files))
		return 1
	}

	split := len(files) - int(float64(len(files))*opt.holdout)
	if split < 1 {
		split = 1
	}
	if split >= len(files) {
		split = len(files) - 1
	}
	train := files[:split]
	replay := files[split:]

	store, err := deps.openStore(opt.db)
	if err != nil {
		// ensure the store directory exists (mirrors runExtract behavior)
		if mkErr := os.MkdirAll(filepath.Dir(opt.db), 0o700); mkErr == nil {
			store, err = deps.openStore(opt.db)
		}
		if err != nil {
			fmt.Fprintf(stdout, "verify: open store: %v\n", err)
			return 1
		}
	}
	idx := deps.newIndex()

	// Train: index every user turn of earlier sessions as a P-slice.
	trained := 0
	for _, path := range train {
		turns, err := parseTurns(path)
		if err != nil {
			fmt.Fprintf(stdout, "verify: %s: %v\n", path, err)
			return 1
		}
		for _, t := range turns {
			sl := &slice.Slice{
				ID:    turnSliceID(t.Query),
				Type:  slice.Prompt,
				Scope: opt.scope,
				Content: []byte(t.Query),
				Meta: slice.SliceMeta{ProjectSlug: opt.project, SourceSession: t.Session},
			}
			if err := store.Put(sl); err != nil {
				fmt.Fprintf(stdout, "verify: put: %v\n", err)
				return 1
			}
			if err := idx.Insert(sl); err != nil {
				fmt.Fprintf(stdout, "verify: index: %v\n", err)
				return 1
			}
			trained++
		}
	}

	// Replay: for every user turn of later sessions, top-1 hit.
	fmt.Fprintf(stdout, "# verify: %d sessions, %d trained turns, %d replay sessions\n", len(files), trained, len(replay))
	fmt.Fprintln(stdout, "# mark each row ✅ (top-1 is a 'previously done similar' turn) or ❌; relevance = marked / total ≥ 0.7")
	fmt.Fprintln(stdout, "session\tturn\tscore\ttop1_content\tquery")

	replayed := 0
	for _, path := range replay {
		turns, err := parseTurns(path)
		if err != nil {
			fmt.Fprintf(stdout, "verify: %s: %v\n", path, err)
			return 1
		}
		for _, t := range turns {
			hits, err := idx.Search(t.Query, 1, opt.scope)
			if err != nil {
				fmt.Fprintf(stdout, "verify: search: %v\n", err)
				return 1
			}
			replayed++
			top1 := ""
			score := 0.0
			if len(hits) > 0 {
				top1 = string(hits[0].Slice.Content)
				score = hits[0].Score
			}
			fmt.Fprintf(stdout, "%s\t%d\t%.4f\t%s\t%s\n",
				t.Session, t.Turn, score, tabSafe(top1), tabSafe(t.Query))
		}
	}
	fmt.Fprintf(stdout, "# done: %d replayed turns; mark rows then compute relevance rate\n", replayed)
	return 0
}

func turnSliceID(q string) string {
	h := sha256.Sum256([]byte(q))
	return "v-" + hex.EncodeToString(h[:8])
}

// tabSafe sanitizes TSV cells: control chars stripped and spreadsheet
// formula-prefix neutralized (= + - @ at cell start -> prefixed with ').
func tabSafe(s string) string {
	s = stripESC(s)
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if strings.HasPrefix(s, "=") || strings.HasPrefix(s, "+") ||
		strings.HasPrefix(s, "-") || strings.HasPrefix(s, "@") {
		s = "'" + s
	}
	return s
}

// stringListFlag collects repeated --session flags.
type stringListFlag struct{ p *[]string }

func (f stringListFlag) String() string { return strings.Join(*f.p, ",") }
func (f stringListFlag) Set(v string) error {
	*f.p = append(*f.p, v)
	return nil
}
