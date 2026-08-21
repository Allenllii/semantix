package bm25

import (
	"reflect"
	"testing"
)

func TestTokenizeLatinCJKAndPunctuation(t *testing.T) {
	got := Tokenize("BM25 检索 cache-first 日本語 한글 Cache_Key42")
	want := []string{"bm25", "检", "索", "cache", "first", "日", "本", "語", "한", "글", "cache_key42"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokenize() = %#v, want %#v", got, want)
	}
}

func TestTokenizeDropsOnlyPunctuation(t *testing.T) {
	if got := Tokenize(" !?，。--- "); len(got) != 0 {
		t.Fatalf("Tokenize() = %#v, want no tokens", got)
	}
}

func TestUniqueTermsKeepsFirstSeenOrder(t *testing.T) {
	got := uniqueTerms([]string{"cache", "测", "cache", "试", "测"})
	want := []string{"cache", "测", "试"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueTerms() = %#v, want %#v", got, want)
	}
}

func TestQueryCoverageFull(t *testing.T) {
	if got := QueryCoverage("fix failing go test", "run go test and fix failing tests"); got != 1.0 {
		t.Fatalf("QueryCoverage = %v, want 1.0", got)
	}
}

func TestQueryCoveragePartial(t *testing.T) {
	if got := QueryCoverage("修复测试", "修复完成"); got != 0.5 {
		t.Fatalf("QueryCoverage = %v, want 0.5", got)
	}
}

func TestQueryCoverageNoTermsFailClosed(t *testing.T) {
	if got := QueryCoverage("?!,。", "anything here"); got != 0 {
		t.Fatalf("QueryCoverage = %v, want 0 (fail-closed)", got)
	}
}

func TestQueryCoverageZeroOverlap(t *testing.T) {
	if got := QueryCoverage("deploy kubernetes cluster", "画一只猫"); got != 0 {
		t.Fatalf("QueryCoverage = %v, want 0", got)
	}
}
