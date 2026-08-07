package main

import (
	"fmt"
	"os"
	"path/filepath"

	"semantix/kernel/slice"
)

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
