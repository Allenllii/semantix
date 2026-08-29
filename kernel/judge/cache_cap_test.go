package judge

import "testing"

// TestVerdictCacheCapEviction: the cache never grows unboundedly — expired
// entries are reclaimed at capacity.
func TestVerdictCacheCapEviction(t *testing.T) {
	c := NewVerdictCache()
	for i := 0; i < cacheCap+50; i++ {
		key := VerdictKey(string(rune('a'+i%26))+"-query-padding", string(rune('a'+i%26))+"-slice-padding")
		c.Set(key, true)
	}
	if c.Len() > cacheCap {
		t.Fatalf("cache grew to %d, cap is %d", c.Len(), cacheCap)
	}
}
