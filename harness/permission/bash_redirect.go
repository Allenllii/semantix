package permission

import "semantix/harness/shellsafe"

func normalizeBashSafeRedirectsForMatch(subject string) (string, bool) {
	return shellsafe.NormalizeBashSafeRedirectsForMatch(subject)
}
