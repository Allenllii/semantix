package main

import "semantix/kernel/config"

// cfgString / cfgInt / cfgFloat read a resolved config entry (M2-U20) with
// a fallback for when the config layer is absent (nil) or the key is unset.
// The resolved value already encodes flag > env > file > default precedence;
// CLI flags override it naturally because flag defaults are only consulted
// when the caller does not pass the flag explicitly.

func cfgString(c *config.Resolved, key, fallback string) string {
	if c == nil {
		return fallback
	}
	if v, _, ok := c.Get(key); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

func cfgInt(c *config.Resolved, key string, fallback int) int {
	if c == nil {
		return fallback
	}
	if v, _, ok := c.Get(key); ok {
		if n, ok := v.(int); ok && n > 0 {
			return n
		}
	}
	return fallback
}

func cfgFloat(c *config.Resolved, key string, fallback float64) float64 {
	if c == nil {
		return fallback
	}
	if v, _, ok := c.Get(key); ok {
		if f, ok := v.(float64); ok && f > 0 {
			return f
		}
	}
	return fallback
}
