package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"semantix/kernel/config"
)

// tomlWithZones writes a semantix.toml with the given [retrieval] tau_*
// keys and resolves it through the config layer.
func tomlWithZones(t *testing.T, body string) *config.Resolved {
	t.Helper()
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := config.Load(config.Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The semantix.toml [retrieval] tau_* keys drive the grey-zone thresholds
// when no explicit --tau-*/--abs-* flag is given (Issue #259 阶段 1).
func TestApplyConfigZonesFromToml(t *testing.T) {
	r := tomlWithZones(t, "[retrieval]\ntau_high = 0.9\ntau_low = 0.6\nabs_high = 0.8\nabs_low = 0.5\n")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	zf := addZoneFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := zf.applyConfigZones(fs, r); err != nil {
		t.Fatal(err)
	}
	z := zf.zones()
	if z.TauHigh != 0.9 || z.TauLow != 0.6 || z.AbsHigh != 0.8 || z.AbsLow != 0.5 {
		t.Fatalf("zones = %+v, want toml values", z)
	}
}

// An explicit --tau-low flag always wins over the toml value.
func TestApplyConfigZonesFlagWins(t *testing.T) {
	r := tomlWithZones(t, "[retrieval]\ntau_low = 0.6\n")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	zf := addZoneFlags(fs)
	if err := fs.Parse([]string{"--tau-low", "0.42"}); err != nil {
		t.Fatal(err)
	}
	if err := zf.applyConfigZones(fs, r); err != nil {
		t.Fatal(err)
	}
	if got := zf.zones().TauLow; got != 0.42 {
		t.Fatalf("TauLow = %v, want explicit flag 0.42", got)
	}
}

// A nil config is a silent no-op (unit tests / absent config layer).
func TestApplyConfigZonesNilSafe(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	zf := addZoneFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := zf.applyConfigZones(fs, nil); err != nil {
		t.Fatal(err)
	}
	def := zf.zones()
	if def.TauHigh != 0.8 || def.TauLow != 0.55 || def.AbsHigh != 0.7 || def.AbsLow != 0.45 {
		t.Fatalf("zones = %+v, want built-in defaults", def)
	}
}

// abs_low = 0 is a legal configuration and must survive the config merge.
func TestApplyConfigZonesKeepsZeroAbsLow(t *testing.T) {
	r := tomlWithZones(t, "[retrieval]\nabs_low = 0\n")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	zf := addZoneFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := zf.applyConfigZones(fs, r); err != nil {
		t.Fatal(err)
	}
	if got := zf.zones().AbsLow; got != 0 {
		t.Fatalf("AbsLow = %v, want explicit 0", got)
	}
}

// Full priority chain: flag > toml > evolve > default. With no flag and no
// toml key, the evolved TauL2 still drives TauLow (Issue #220 preserved).
func TestApplyConfigZonesThenEvolve(t *testing.T) {
	r := tomlWithZones(t, "[retrieval]\ntau_high = 0.85\n") // tau_low absent
	dir := t.TempDir()
	writeParams(t, dir, 0.75)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	zf := addZoneFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := zf.applyConfigZones(fs, r); err != nil {
		t.Fatal(err)
	}
	if err := zf.applyEvolveParams(fs, dir); err != nil {
		t.Fatal(err)
	}
	z := zf.zones()
	if z.TauHigh != 0.85 {
		t.Fatalf("TauHigh = %v, want toml 0.85", z.TauHigh)
	}
	if z.TauLow != 0.75 {
		t.Fatalf("TauLow = %v, want evolved 0.75", z.TauLow)
	}
}
