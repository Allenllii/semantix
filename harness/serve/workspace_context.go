package serve

import (
	"context"
	"strings"
	"time"

	"semantix/harness/gitcmd"
)

const workspaceGitProbeTimeout = 700 * time.Millisecond

// currentWorkspaceBranch is a best-effort read-only identity probe. The
// workspace still works for non-Git directories; an empty result is rendered
// as an explicit unavailable state by the browser client.
func currentWorkspaceBranch(ctx context.Context, root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	probeCtx, cancel := context.WithTimeout(ctx, workspaceGitProbeTimeout)
	defer cancel()
	if out, err := gitcmd.Command(probeCtx, root, "symbolic-ref", "--quiet", "--short", "HEAD").Output(); err == nil {
		if branch := strings.TrimSpace(string(out)); branch != "" {
			return branch
		}
	}
	if out, err := gitcmd.Command(probeCtx, root, "rev-parse", "--short", "HEAD").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}
