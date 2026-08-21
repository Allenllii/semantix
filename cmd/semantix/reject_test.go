package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/config"
	"semantix/kernel/slice"
)

func rejectTestDeps(db string) dependencies {
	cfg := &config.Resolved{Fields: []config.Field{
		{Key: "store.db", Value: db, Source: config.SourceFile},
	}}
	return dependencies{openStore: slice.NewFileStore, resolved: cfg}
}

func TestRejectMarksSlice(t *testing.T) {
	db := filepath.Join(t.TempDir(), "s.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(&slice.Slice{ID: "s1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	closeStore(store)

	var out bytes.Buffer
	if err := runReject([]string{"s1"}, &out, &emptyStderr{}, rejectTestDeps(db)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "rejected slice s1") {
		t.Fatalf("output = %q, want rejection notice", out.String())
	}

	// Re-open so the rejection is verified through the store contract, not
	// the in-memory handle the command closed.
	reopened, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	closeStore(reopened)
	got, err := reopened.Get("s1")
	if err != nil || got == nil {
		t.Fatalf("Get after reject: %v %v", got, err)
	}
	if got.Stats.Rejected != 1 || got.Stats.UserFeedback != -1 {
		t.Fatalf("stats = %+v, want Rejected=1 UserFeedback=-1", got.Stats)
	}
}

func TestRejectUnknownSliceFails(t *testing.T) {
	db := filepath.Join(t.TempDir(), "s.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	closeStore(store)
	var out bytes.Buffer
	if err := runReject([]string{"ghost"}, &out, &emptyStderr{}, rejectTestDeps(db)); err == nil {
		t.Fatal("reject of unknown slice must fail (errNotFound)")
	}
}

func TestRejectJSONEnvelope(t *testing.T) {
	db := filepath.Join(t.TempDir(), "s.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(&slice.Slice{ID: "s2", Type: slice.Prompt, Scope: slice.Project, Content: []byte("y")}); err != nil {
		t.Fatal(err)
	}
	closeStore(store)
	// NOTE: s2 case closes before runReject; the JSON case reopens via deps.

	var out bytes.Buffer
	if err := runReject([]string{"s2", "--reason", "wrong advice", "--json"}, &out, &emptyStderr{}, rejectTestDeps(db)); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %v", err)
	}
	if env["ok"] != true || env["command"] != "reject" {
		t.Fatalf("envelope = %v, want ok=true command=reject", env)
	}
	data := env["data"].(map[string]any)
	if data["slice_id"] != "s2" || data["reason"] != "wrong advice" || data["rejected"] != true {
		t.Fatalf("data = %v", data)
	}
}

func TestRejectRequiresExactlyOneArg(t *testing.T) {
	db := filepath.Join(t.TempDir(), "s.db")
	for _, args := range [][]string{{}, {"a", "b"}} {
		var out bytes.Buffer
		if err := runReject(args, &out, &emptyStderr{}, rejectTestDeps(db)); err == nil {
			t.Fatalf("args %v must be a usage error", args)
		}
	}
}
