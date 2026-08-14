package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/config"
	"semantix/kernel/slice"
)

// M2-U20: 命令默认值从 config 读取（flag > env > file > default）。
// 注入一个 config.Resolved（等价 semantix.toml 已解析），验证 lookup 的
// --db/--limit 默认值不再来自硬编码，而是来自 config。
func TestConfigDefaultsWiredIntoCommands(t *testing.T) {
	dir := t.TempDir()
	customDB := filepath.Join(dir, "custom.db")
	store, err := slice.NewFileStore(customDB)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range []string{"修复 go 测试失败", "修复 go 测试超时", "修复 go 测试崩溃"} {
		sl := &slice.Slice{ID: fmt.Sprintf("c%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte(q)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Resolved{Fields: []config.Field{
		{Key: "store.db", Value: customDB, Source: config.SourceFile},
		{Key: "retrieval.limit", Value: 2, Source: config.SourceFile},
	}}
	deps := dependencies{
		newExtractor: slice.NewExtractor,
		openStore:    slice.NewFileStore,
		newIndex:     func() slice.Index { return bm25.New() },
		resolved:     cfg,
	}

	// lookup 不带 --db/--limit：db 默认取 config.store.db（custom.db），
	// limit 默认取 config.retrieval.limit（2，而非硬编码 5）。
	// 用 --json 信封读取结果数（U30 起默认输出为人类可读文本）。
	var out bytes.Buffer
	if err := runLookup([]string{"--query", "修复 go 测试", "--json"}, &out, &emptyStderr{}, deps); err != nil {
		t.Fatalf("lookup with config defaults: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	data, ok := env.Data.([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("results = %d, want 2 (config retrieval.limit + store.db applied)", len(data))
	}
}

// M2-U20: 非法 semantix.toml → 退出码 2 + 字段定位信息（§4.3 usage class）。
func TestRunInvalidConfigExit2(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "semantix.toml"), []byte("store = { db = "), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	code := run([]string{"version"}, &out, &errOut, productionDependencies())
	if code != 2 {
		t.Fatalf("code = %d, want 2 (invalid config); stderr: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "invalid config") {
		t.Fatalf("stderr missing 'invalid config': %q", errOut.String())
	}
}
