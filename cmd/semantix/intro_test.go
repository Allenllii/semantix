package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunDispatchesIntro(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"intro", "--no-animation"}, &stdout, &stderr, dependencies{}); code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	// Unit builds carry no ldflags, so the banner shows the "dev" fallback.
	if !strings.Contains(stdout.String(), "Semantix dev") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestIntroPassesBuildVersionToBanner(t *testing.T) {
	t.Cleanup(func(prev string) func() { return func() { version = prev } }(version))
	version = "v9.9.9-test"
	var stdout, stderr bytes.Buffer
	if code := run([]string{"intro", "--no-animation"}, &stdout, &stderr, dependencies{}); code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Semantix v9.9.9-test") {
		t.Fatalf("banner did not use the injected build version: %q", stdout.String())
	}
}
