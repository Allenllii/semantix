package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"semantix/kernel/evolve"
)

// writeParams persists an evolveState with the given TauL2 for consumer tests.
func writeParams(t *testing.T, dir string, tau float64) {
	t.Helper()
	b, err := json.Marshal(evolveState{Params: evolve.Params{TauL2: tau}, Epoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "params.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newZoneFlagSet(t *testing.T, args ...string) (*flag.FlagSet, *zoneFlagSet) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	zf := addZoneFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs, zf
}

// The evolved TauL2 becomes the grey-zone floor when --tau-low is not given
// (Issue #220: params.json gains a consumer).
func TestApplyEvolveParamsOverridesDefaultTauLow(t *testing.T) {
	dir := t.TempDir()
	writeParams(t, dir, 0.75)
	fs, zf := newZoneFlagSet(t)
	if err := zf.applyEvolveParams(fs, dir); err != nil {
		t.Fatal(err)
	}
	if got := zf.zones().TauLow; got != 0.75 {
		t.Fatalf("TauLow = %v, want evolved 0.75", got)
	}
	if err := zf.validate(); err != nil {
		t.Fatalf("evolved value must pass validation: %v", err)
	}
}

// An explicit --tau-low always beats the evolved value.
func TestApplyEvolveParamsExplicitFlagWins(t *testing.T) {
	dir := t.TempDir()
	writeParams(t, dir, 0.75)
	fs, zf := newZoneFlagSet(t, "--tau-low", "0.40")
	if err := zf.applyEvolveParams(fs, dir); err != nil {
		t.Fatal(err)
	}
	if got := zf.zones().TauLow; got != 0.40 {
		t.Fatalf("TauLow = %v, explicit flag must win", got)
	}
}

// Values outside the engine's tuning band are clamped so a hand-edited file
// cannot push the classifier into extremes evolve itself could not reach.
func TestApplyEvolveParamsClampsToTuningBand(t *testing.T) {
	dir := t.TempDir()
	writeParams(t, dir, 0.99)
	fs, zf := newZoneFlagSet(t)
	if err := zf.applyEvolveParams(fs, dir); err != nil {
		t.Fatal(err)
	}
	if got := zf.zones().TauLow; got != evolve.DefaultMaxTau {
		t.Fatalf("TauLow = %v, want clamped to %v", got, evolve.DefaultMaxTau)
	}
}

// Empty dir flag and missing state file both leave the defaults untouched.
func TestApplyEvolveParamsFailOpen(t *testing.T) {
	fs, zf := newZoneFlagSet(t)
	if err := zf.applyEvolveParams(fs, ""); err != nil {
		t.Fatal(err)
	}
	if err := zf.applyEvolveParams(fs, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := zf.zones().TauLow; got != 0.55 {
		t.Fatalf("TauLow = %v, want untouched default 0.55", got)
	}
}
