package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunVersionHuman(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "version\tdev") {
		t.Fatalf("stdout = %q, want version\\tdev", out)
	}
	if !strings.Contains(out, "commit\tunknown") {
		t.Fatalf("stdout = %q, want commit\\tunknown", out)
	}
	// build_time is empty by default and must be omitted
	if strings.Contains(out, "build_time") {
		t.Fatalf("stdout = %q, empty build_time should be omitted", out)
	}
}

func TestRunVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "--json"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, stdout.String())
	}
	if !env.OK || env.Command != "version" {
		t.Fatalf("envelope = %#v", env)
	}
	if env.Error != nil {
		t.Fatalf("envelope error = %#v", env.Error)
	}
}

func TestRunVersionJSONFields(t *testing.T) {
	oldVersion, oldCommit, oldBuild := version, commit, buildTime
	defer func() { version, commit, buildTime = oldVersion, oldCommit, oldBuild }()
	version, commit, buildTime = "v0.3.1", "abc1234", "2026-08-14T00:00:00Z"

	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "--json"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	var env struct {
		Data struct {
			Version   string `json:"version"`
			Commit    string `json:"commit"`
			BuildTime string `json:"build_time"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Data.Version != "v0.3.1" || env.Data.Commit != "abc1234" || env.Data.BuildTime != "2026-08-14T00:00:00Z" {
		t.Fatalf("data = %#v", env.Data)
	}
}

func TestRunVersionUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "--nope"}, &stdout, &stderr, dependencies{})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestRunVersionHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "--help"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
}
