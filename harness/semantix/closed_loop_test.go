package semantix

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	kernelevent "semantix/kernel/event"
)

func TestBridgePersistsPrefetchAndEvolutionEventsInSessionJSONL(t *testing.T) {
	dir := t.TempDir()
	b := NewBridge(Config{Enabled: true, SessionsDir: dir})
	b.SetLabel("session-c4")
	for turn := 1; turn <= 60; turn++ {
		b.RecordPrefetch(false, []string{"slice-a"}, turn)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "session-c4.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	counts := map[kernelevent.Kind]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		e, err := kernelevent.FromJSON(sc.Bytes())
		if err == nil {
			counts[e.Kind]++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if counts[kernelevent.PrefetchWaste] != 60 {
		t.Fatalf("waste=%d", counts[kernelevent.PrefetchWaste])
	}
	if counts[kernelevent.EvolutionTick] == 0 {
		t.Fatalf("tick=%d, want >0", counts[kernelevent.EvolutionTick])
	}
}
