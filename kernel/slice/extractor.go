package slice

// Extractor turns session transcripts into slices (MVP implementation in U4).
type Extractor interface {
	// Extract processes one session transcript (JSONL lines) and returns new
	// or updated slices ready for store.Put.
	Extract(sessionJSONL []byte, meta SliceMeta) ([]*Slice, error)
}

// NewExtractor returns the default extractor (U4 implementation).
func NewExtractor() Extractor { return nil } // placeholder until U4
