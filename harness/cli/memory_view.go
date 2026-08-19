package cli

import (
	"time"

	"semantix/harness/control"
	"semantix/harness/memory"
)

func renderMemory(width int, set *memory.Set) string {
	return viewProtectLines(control.RenderMemorySummary(set, time.Now().UTC()), width)
}
