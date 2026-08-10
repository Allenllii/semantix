package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// zoneFlagSet registers the grey-zone threshold flags (Issue #7) and rebuilds
// a zone.Zones after flag.Parse. All four default to zone.Default().
type zoneFlagSet struct {
	tauHigh *float64
	tauLow  *float64
	absHigh *float64
	absLow  *float64
}

func addZoneFlags(fs *flag.FlagSet) *zoneFlagSet {
	d := zone.Default()
	return &zoneFlagSet{
		tauHigh: fs.Float64("tau-high", d.TauHigh, "relative confidence for clear hit (score/top1)"),
		tauLow:  fs.Float64("tau-low", d.TauLow, "relative confidence for grey zone"),
		absHigh: fs.Float64("abs-high", d.AbsHigh, "absolute score floor for clear hit"),
		absLow:  fs.Float64("abs-low", d.AbsLow, "absolute score floor for grey zone"),
	}
}

func (zf *zoneFlagSet) zones() zone.Zones {
	return zone.Zones{
		TauHigh: *zf.tauHigh,
		TauLow:  *zf.tauLow,
		AbsHigh: *zf.absHigh,
		AbsLow:  *zf.absLow,
	}
}

func parseScope(value string) (slice.Scope, error) {
	switch value {
	case "session":
		return slice.Session, nil
	case "project":
		return slice.Project, nil
	case "user":
		return slice.User, nil
	default:
		return 0, fmt.Errorf("invalid scope %q (want session, project, or user)", value)
	}
}

func scopeName(scope slice.Scope) string {
	switch scope {
	case slice.Session:
		return "session"
	case slice.Project:
		return "project"
	case slice.User:
		return "user"
	default:
		return fmt.Sprintf("scope(%d)", scope)
	}
}

func defaultProjectDB() string {
	return filepath.Join(".semantix", "project.db")
}

func defaultUserDB() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".semantix", "user.db")
	}
	return filepath.Join(home, ".semantix", "user.db")
}

func selectDB(scope slice.Scope, override, projectDB, userDB string) string {
	if override != "" {
		return override
	}
	if scope == slice.User {
		return userDB
	}
	return projectDB
}
