package slice

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"time"
)

// rawExporter is implemented by fileStore: full-fidelity v1 JSONL lines (raw
// embedding bytes preserved even though reads return Embedding=nil) plus the
// corrupt-line count. Other Store implementations fall back to ListAll and
// report zero skipped.
type rawExporter interface {
	exportLines() ([][]byte, int, error)
}

// Export writes every slice in the store to w as JSONL — one full Slice JSON
// per line, including Meta, Stats, Weight and CreatedAt. The output is a
// complete backup and a valid Import input (export/import round-trip is
// lossless). Returns the number of slices written and how many store lines
// were corrupt and dropped (so a "lossless backup" is never silently
// incomplete).
func Export(store Store, w io.Writer) (count, skipped int, err error) {
	if re, ok := store.(rawExporter); ok {
		lines, skipped, err := re.exportLines()
		if err != nil {
			return 0, skipped, err
		}
		n := 0
		for _, line := range lines {
			if _, err := w.Write(append(line, '\n')); err != nil {
				return n, skipped, err
			}
			n++
		}
		return n, skipped, nil
	}
	all, err := store.ListAll()
	if err != nil {
		return 0, 0, err
	}
	n := 0
	for _, sl := range all {
		b, err := json.Marshal(sl)
		if err != nil {
			return n, 0, err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return n, 0, err
		}
		n++
	}
	return n, 0, nil
}

// maxImportLine bounds a single accepted JSONL line; longer lines are treated
// as corrupt input and skipped (counted), never fatal. Matches the store's
// own 8 MiB tolerance (fileStore.readAll).
const maxImportLine = 8 * 1024 * 1024

// Import reads JSONL produced by Export (or any JSONL store file) and stores
// each slice. Idempotent: Put replaces by ID, so re-importing the same backup
// converges to the same store state without duplicates. Corrupt, ID-less and
// oversized (> 8 MiB) lines are skipped and counted, so one bad line never
// aborts a restore. Returns imported and skipped counts.
func Import(store Store, r io.Reader) (imported, skipped int, err error) {
	br := bufio.NewReader(r)
	for {
		line, tooLong, err := readJSONLLine(br)
		if err == io.EOF {
			return imported, skipped, nil // clean end of stream
		}
		if err != nil {
			return imported, skipped, err
		}
		if tooLong {
			// Oversized lines are corrupt input: skip and keep going.
			skipped++
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var sl Slice
		if err := json.Unmarshal(line, &sl); err != nil {
			skipped++
			continue
		}
		if sl.ID == "" {
			// An unkeyed slice would clobber every other empty-ID slice
			// in the store; refuse it rather than corrupt data silently.
			skipped++
			continue
		}
		if err := store.Put(&sl); err != nil {
			return imported, skipped, err
		}
		imported++
	}
}

// readJSONLLine reads one logical line (up to and including '\n') from br.
// A line longer than maxImportLine is drained (skipped) and reported via
// tooLong=true so the import continues with the following lines. Returns
// io.EOF when the stream ends cleanly at a line boundary.
func readJSONLLine(br *bufio.Reader) (line []byte, tooLong bool, err error) {
	var full []byte
	for {
		frag, ferr := br.ReadSlice('\n')
		full = append(full, frag...)
		if len(full) > maxImportLine {
			// Drain the remainder of the oversized line, then report skip.
			for ferr == bufio.ErrBufferFull {
				_, ferr = br.ReadSlice('\n')
			}
			return nil, true, nil
		}
		if ferr == bufio.ErrBufferFull {
			continue
		}
		if ferr == io.EOF && len(full) == 0 {
			return nil, false, io.EOF
		}
		// ferr is nil (line ended with '\n') or io.EOF with trailing data.
		return full, false, nil
	}
}

// GCOptions configures a maintenance GC pass. Zero values disable the
// corresponding criterion, so a bare `GC(store, GCOptions{})` removes nothing.
type GCOptions struct {
	// RetentionDays: slices older than this many days are expired. Slices
	// with unknown (zero) CreatedAt are never expired by retention.
	RetentionDays int
	// MinWeight: slices whose Weight is strictly below this are low-score.
	// Slices with zero Weight are treated as unknown (legacy, pre-U26 data
	// never went through the evolution engine) and are never removed by the
	// score criterion — same protection as unknown CreatedAt.
	MinWeight float64
	// DryRun reports candidates without deleting anything.
	DryRun bool
}

// GCResult summarizes one GC pass.
type GCResult struct {
	Checked  int      // slices inspected
	Removed  int      // slices removed (or that would be removed in dry-run)
	Expired  []string // ids expired by retention
	LowScore []string // ids below the weight threshold
}

// GC removes expired / low-score slices from the store. A slice is a candidate
// when it matches the retention criterion OR the score criterion; ids appear
// in the matching list(s) but are only removed once. With DryRun nothing is
// deleted and Removed reports the candidate count.
func GC(store Store, opts GCOptions) (GCResult, error) {
	var res GCResult
	all, err := store.ListAll()
	if err != nil {
		return res, err
	}
	res.Checked = len(all)
	cutoff := time.Now().Add(-time.Duration(opts.RetentionDays) * 24 * time.Hour).Unix()
	for _, sl := range all {
		expired := opts.RetentionDays > 0 && sl.CreatedAt > 0 && sl.CreatedAt < cutoff
		// Weight==0 means "never scored" (legacy/unknown), not "worthless":
		// protect it like unknown CreatedAt so a threshold run cannot wipe
		// the pre-U26 library.
		low := opts.MinWeight > 0 && sl.Weight > 0 && sl.Weight < opts.MinWeight
		if expired {
			res.Expired = append(res.Expired, sl.ID)
		}
		if low {
			res.LowScore = append(res.LowScore, sl.ID)
		}
		if !expired && !low {
			continue
		}
		res.Removed++
		if !opts.DryRun {
			if err := store.Delete(sl.ID); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}
