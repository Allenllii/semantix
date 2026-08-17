package agent

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"semantix/harness/tool"
	kernelevent "semantix/kernel/event"
)

// resourceCatalogState owns the harness-side full snapshot. Scheduler
// suspension is declarative: setSuspended replaces the previous set, so an
// omitted tool is recovered immediately on the next round.
type resourceCatalogState struct {
	mu        sync.Mutex
	bus       kernelevent.Bus
	models    []kernelevent.ResourceModel
	budget    kernelevent.ResourceBudget
	suspended map[string]struct{}
	toolRev   uint64
	tools     []kernelevent.ResourceTool
}

func newResourceCatalogState(bus kernelevent.Bus, tools *tool.Registry, models []kernelevent.ResourceModel, budget kernelevent.ResourceBudget) *resourceCatalogState {
	r := &resourceCatalogState{
		bus:       bus,
		models:    append([]kernelevent.ResourceModel(nil), models...),
		budget:    budget,
		suspended: make(map[string]struct{}),
	}
	sort.Slice(r.models, func(i, j int) bool {
		if r.models[i].Tier == r.models[j].Tier {
			return r.models[i].ID < r.models[j].ID
		}
		return r.models[i].Tier < r.models[j].Tier
	})
	r.refreshToolsLocked(tools)
	r.emitLocked() // startup full snapshot
	return r
}

func (r *resourceCatalogState) schedulingState(tools *tool.Registry) ([]string, kernelevent.ResourceBudget) {
	if r == nil {
		return nil, kernelevent.ResourceBudget{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if tools != nil && tools.SchemaRevision() != r.toolRev {
		r.refreshToolsLocked(tools)
		r.emitLocked()
	}
	names := make([]string, 0, len(r.suspended))
	for name := range r.suspended {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, r.budget
}

func (r *resourceCatalogState) setSuspended(tools *tool.Registry, names []string) {
	if r == nil {
		return
	}
	next := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name != "" {
			next[name] = struct{}{}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := !sameNameSet(r.suspended, next)
	r.suspended = next
	if tools != nil {
		r.refreshToolsLocked(tools)
	}
	if changed {
		r.emitLocked()
	}
}

func (r *resourceCatalogState) setBudget(tools *tool.Registry, budget kernelevent.ResourceBudget) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.budget == budget {
		return
	}
	r.budget = budget
	r.refreshToolsLocked(tools)
	r.emitLocked()
}

func (r *resourceCatalogState) refresh(tools *tool.Registry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshToolsLocked(tools)
	r.emitLocked()
}

func (r *resourceCatalogState) refreshToolsLocked(reg *tool.Registry) {
	if reg == nil {
		r.tools = nil
		r.toolRev = 0
		return
	}
	names := reg.AllNames()
	sort.Strings(names)
	items := make([]kernelevent.ResourceTool, 0, len(names))
	for _, name := range names {
		t, ok := reg.Get(name)
		if !ok || t == nil {
			continue
		}
		_, suspended := r.suspended[name]
		items = append(items, kernelevent.ResourceTool{Name: name, ReadOnly: t.ReadOnly(), Suspended: suspended})
	}
	r.tools = items
	r.toolRev = reg.SchemaRevision()
}

func (r *resourceCatalogState) emitLocked() {
	if r.bus == nil {
		return
	}
	payload := kernelevent.ResourceCatalogPayload{
		Tools:  append([]kernelevent.ResourceTool(nil), r.tools...),
		Models: append([]kernelevent.ResourceModel(nil), r.models...),
		Budget: r.budget,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	r.bus.Emit(kernelevent.Event{Kind: kernelevent.ResourceCatalog, At: time.Now().UTC(), Data: data})
}

func sameNameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			return false
		}
	}
	return true
}

func (a *Agent) resourceSchedulingState() ([]string, kernelevent.ResourceBudget) {
	if a == nil || a.resources == nil {
		return nil, kernelevent.ResourceBudget{}
	}
	return a.resources.schedulingState(a.svc.tools)
}

func (a *Agent) syncResourceCatalog() {
	if a != nil && a.resources != nil {
		_, _ = a.resources.schedulingState(a.svc.tools)
	}
}

// SetResourceBudget replaces the catalog's budget snapshot and emits the full
// catalog. U41's BudgetController uses this seam; enforcement remains local.
func (a *Agent) SetResourceBudget(budget kernelevent.ResourceBudget) {
	if a != nil && a.resources != nil {
		a.resources.setBudget(a.svc.tools, budget)
	}
}

// RefreshResourceCatalog re-emits a full snapshot after dynamic tool changes.
func (a *Agent) RefreshResourceCatalog() {
	if a != nil && a.resources != nil {
		a.resources.refresh(a.svc.tools)
	}
}
