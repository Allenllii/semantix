package bm25

import (
	"reflect"
	"testing"
)

func TestTokenizeLatinCJKAndPunctuation(t *testing.T) {
	got := tokenize("BM25 检索 cache-first 日本語 한글 Cache_Key42")
	want := []string{"bm25", "检", "索", "cache", "first", "日", "本", "語", "한", "글", "cache_key42"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokenize() = %#v, want %#v", got, want)
	}
}

func TestTokenizeDropsOnlyPunctuation(t *testing.T) {
	if got := tokenize(" !?，。--- "); len(got) != 0 {
		t.Fatalf("tokenize() = %#v, want no tokens", got)
	}
}

func TestUniqueTermsKeepsFirstSeenOrder(t *testing.T) {
	got := uniqueTerms([]string{"cache", "测", "cache", "试", "测"})
	want := []string{"cache", "测", "试"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueTerms() = %#v, want %#v", got, want)
	}
}
