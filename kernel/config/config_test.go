package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertField(t *testing.T, r *Resolved, key string, want any, wantSrc Source) {
	t.Helper()
	v, src, ok := r.Get(key)
	if !ok {
		t.Fatalf("field %q missing", key)
	}
	if v != want {
		t.Errorf("field %q = %v (%T), want %v (%T)", key, v, v, want, want)
	}
	if src != wantSrc {
		t.Errorf("field %q source = %q, want %q", key, src, wantSrc)
	}
}

func TestLoadDefaults(t *testing.T) {
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "project.name", "my-project", SourceDefault)
	assertField(t, r, "store.db", ".semantix/project.db", SourceDefault)
	assertField(t, r, "store.scope", "project", SourceDefault)
	assertField(t, r, "retrieval.retriever", "hybrid", SourceDefault)
	assertField(t, r, "retrieval.limit", 5, SourceDefault)
	assertField(t, r, "inject.budget", 4096, SourceDefault)
	assertField(t, r, "verify.holdout", 0.3, SourceDefault)
	assertField(t, r, "cost.output_price_usd", 1.10, SourceDefault)
}

func TestLoadFileOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	content := "[store]\ndb = \"/tmp/custom.db\"\nscope = \"user\"\n\n[retrieval]\nlimit = 20\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "/tmp/custom.db", SourceFile)
	assertField(t, r, "store.scope", "user", SourceFile)
	assertField(t, r, "retrieval.limit", 20, SourceFile)
	// keys the file did not define fall back to default
	assertField(t, r, "retrieval.retriever", "hybrid", SourceDefault)
	assertField(t, r, "inject.budget", 4096, SourceDefault)
}

func TestLoadPartialSectionFallsBackToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[store]\ndb = \"/tmp/only-db.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "/tmp/only-db.db", SourceFile)
	assertField(t, r, "store.scope", "project", SourceDefault)
	assertField(t, r, "retrieval.limit", 5, SourceDefault)
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("SEMANTIX_DB", "/env/user.db")
	t.Setenv("SEMANTIX_LIMIT", "42")
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "/env/user.db", SourceEnv)
	assertField(t, r, "retrieval.limit", 42, SourceEnv)
}

func TestLoadEnvEmptyStringFallsThrough(t *testing.T) {
	t.Setenv("SEMANTIX_DB", "")
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", ".semantix/project.db", SourceDefault)
}

func TestLoadFlagOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[store]\ndb = \"/file.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEMANTIX_DB", "/env.db")
	r, err := Load(Options{ConfigPath: path, DB: "/flag.db"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// flag beats env beats file
	assertField(t, r, "store.db", "/flag.db", SourceFlag)
}

func TestLoadEnvBeatsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[store]\ndb = \"/file.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEMANTIX_DB", "/env.db")
	r, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "/env.db", SourceEnv)
}

func TestLoadInvalidScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[store]\nscope = \"team\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(Options{ConfigPath: path})
	if err == nil {
		t.Fatal("Load: want error for invalid scope")
	}
	if !strings.Contains(err.Error(), "store.scope") {
		t.Fatalf("error %q should locate store.scope", err)
	}
}

func TestLoadInvalidEnvInteger(t *testing.T) {
	t.Setenv("SEMANTIX_LIMIT", "abc")
	_, err := Load(Options{})
	if err == nil {
		t.Fatal("Load: want error for invalid env integer")
	}
	if !strings.Contains(err.Error(), "retrieval.limit") {
		t.Fatalf("error %q should locate retrieval.limit", err)
	}
}

func TestLoadTypeErrorReportsLocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[retrieval]\nlimit = \"not-a-number\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(Options{ConfigPath: path})
	if err == nil {
		t.Fatal("Load: want error for string into int field")
	}
	// BurntSushi/toml reports line/column in its error
	if !strings.Contains(err.Error(), "toml") && !strings.Contains(err.Error(), "line") {
		t.Fatalf("error %q should carry location info", err)
	}
}

func TestLoadExplicitMissingFile(t *testing.T) {
	_, err := Load(Options{ConfigPath: filepath.Join(t.TempDir(), "nope.toml")})
	if err == nil {
		t.Fatal("Load: want error for explicit missing file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error %q should report missing file", err)
	}
}

func TestLoadDefaultMissingFileSkipped(t *testing.T) {
	// default ./semantix.toml does not exist in the test cwd → skip, not error
	dir := t.TempDir()
	t.Chdir(dir)
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", ".semantix/project.db", SourceDefault)
}
