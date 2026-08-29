package config

func normalizeOpenAIReasoningEffort(e *ProviderEntry, level string) (string, error) {
	if isMimoEntry(e) {
		switch level {
		case "none", "low", "medium", "high":
			return level, nil
		default:
			return "", &UnsupportedEffortError{Levels: []string{"auto", "none", "low", "medium", "high"}}
		}
	}
	switch level {
	case "low", "medium", "high":
		return level, nil
	default:
		return "", &UnsupportedEffortError{Levels: []string{"auto", "low", "medium", "high"}}
	}
}

func normalizeKimiK3ReasoningEffort(level string) (string, error) {
	if isKimiK3ReasoningEffort(level) {
		return level, nil
	}
	return "", &UnsupportedEffortError{Levels: []string{"auto", "low", "high", "max"}}
}

func isKimiK3ReasoningEffort(level string) bool {
	switch level {
	case "low", "high", "max":
		return true
	default:
		return false
	}
}
