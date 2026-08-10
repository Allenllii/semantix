package embed

// Embedder converts texts into vectors. MVP uses NoopEmbedder (BM25-only
// degradation); a local model (Ollama/bge-m3) can be wired later.
type Embedder interface {
	Embed(texts []string) ([][]float32, error)
}

// NoopEmbedder returns nil vectors without error (callers must fall back to
// BM25 when vectors are absent).
type NoopEmbedder struct{}

// Embed implements Embedder for NoopEmbedder.
func (NoopEmbedder) Embed(texts []string) ([][]float32, error) {
	return nil, nil
}
