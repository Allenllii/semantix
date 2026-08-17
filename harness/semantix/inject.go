package semantix

import (
	"context"
	"fmt"
)

// Inject runs the kernel's L2 injector and returns the [semantix-reuse] block
// for query, or "" when the kernel is unavailable/times out (soft degrade).
func Inject(ctx context.Context, binary, query string, budget int) string {
	if query == "" {
		return ""
	}
	args := []string{"inject", "--query", query}
	if budget > 0 {
		args = append(args, "--budget", fmt.Sprint(budget))
	}
	out, err := runCLI(ctx, binary, "", args)
	if err != nil {
		return ""
	}
	return string(out)
}
