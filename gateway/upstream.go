package gateway

import (
	"sort"
)

// upstreamSet routes client-visible model names to configured upstreams
// (design §4.2: New API channel model names are the gateway's model_alias).
type upstreamSet struct {
	byAlias map[string]*Upstream
	list    []Upstream
}

func newUpstreamSet(ups []Upstream) *upstreamSet {
	s := &upstreamSet{byAlias: make(map[string]*Upstream, len(ups)), list: ups}
	for i := range ups {
		for _, a := range ups[i].ModelAlias {
			s.byAlias[a] = &ups[i]
		}
	}
	return s
}

// resolve finds the upstream serving a client-visible model name.
func (s *upstreamSet) resolve(model string) (*Upstream, bool) {
	up, ok := s.byAlias[model]
	return up, ok
}

// aliases lists every routable model name, sorted for deterministic output.
func (s *upstreamSet) aliases() []string {
	out := make([]string, 0, len(s.byAlias))
	for a := range s.byAlias {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// upstreamModel maps a client-visible model name to the real upstream model
// (design §4.2 mapping target; empty means pass-through).
func (s *upstreamSet) upstreamModel(model string) string {
	if up, ok := s.byAlias[model]; ok && up.UpstreamModel != "" {
		return up.UpstreamModel
	}
	return model
}
