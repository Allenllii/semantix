package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"semantix/kernel/usage"
)

// newBillingGateway builds a gateway with the free-tier gate enabled.
// freeTokens uses the config default (10M) when 0, mirroring production.
func newBillingGateway(t *testing.T, upURL string, billing BillingConfig) *Gateway {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Ingest.UsageLog = filepath.Join(dir, "usage.jsonl")
	cfg.Billing = billing
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: upURL, APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// usagePlainResponse is a canned upstream reply carrying exact provider
// usage (prompt 60 + completion 40 = 100 tokens per call).
const usagePlainResponse = `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"deepseek-chat",` +
	`"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],` +
	`"usage":{"prompt_tokens":60,"completion_tokens":40,"total_tokens":100}}`

func billingBase(t *testing.T) (srvURL string, cleanup func()) {
	up := &testUpstream{plain: usagePlainResponse}
	upstream := httptest.NewServer(up.handler())
	return upstream.URL, upstream.Close
}

func TestQuotaEngineConsumePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-state.json")
	cfg := BillingConfig{Enabled: true, FreeTokens: 1000, RechargeURL: "https://pay.example/topup"}
	q, err := newQuotaEngine(cfg, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Consume(120); err != nil {
		t.Fatal(err)
	}
	if err := q.Consume(80); err != nil {
		t.Fatal(err)
	}
	// negative/zero consumption is ignored
	if err := q.Consume(0); err != nil {
		t.Fatal(err)
	}
	q2, err := newQuotaEngine(cfg, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := q2.Status(context.Background())
	if st.UsedTokens != 200 || st.RemainingTokens != 800 || st.Mode != quotaModeFree {
		t.Fatalf("after restart: %+v", st)
	}
}

func TestQuotaEngineCorruptStateFailsStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := BillingConfig{Enabled: true, FreeTokens: 1000, RechargeURL: "https://pay.example/topup"}
	if _, err := newQuotaEngine(cfg, path, nil, nil); err == nil {
		t.Fatal("corrupt state must fail startup, not silently re-grant the tier")
	}
}

func TestQuotaFreeTierMetersAndSetsHeaders(t *testing.T) {
	upURL, done := billingBase(t)
	defer done()
	g := newBillingGateway(t, upURL, BillingConfig{
		Enabled: true, FreeTokens: 1000, RechargeURL: "https://pay.example/topup",
	})
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi there", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("x-semantix-quota-mode"); got != quotaModeFree {
		t.Fatalf("mode header %q", got)
	}
	if got := resp.Header.Get("x-semantix-quota-limit"); got != "1000" {
		t.Fatalf("limit header %q", got)
	}
	st := g.quota.Status(context.Background())
	if st.UsedTokens != 100 {
		t.Fatalf("used %d after one 100-token exchange", st.UsedTokens)
	}
	// counter survives a restart on the same store
	q2, err := newQuotaEngine(g.cfg.Billing, filepath.Join(filepath.Dir(g.cfg.Store.DB), "quota-state.json"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := q2.Status(context.Background()).UsedTokens; got != 100 {
		t.Fatalf("persisted used %d", got)
	}
}

func TestQuotaExhaustedBlocksWith402(t *testing.T) {
	upURL, done := billingBase(t)
	defer done()
	g := newBillingGateway(t, upURL, BillingConfig{
		Enabled: true, FreeTokens: 150, RechargeURL: "https://pay.example/topup",
	})
	srv := httptest.NewServer(g)
	defer srv.Close()

	// first exchange fits (100 of 150), second is admitted while remaining
	// > 0 and pushes the meter past the tier, third is blocked.
	for i := 0; i < 2; i++ {
		resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d", i, resp.StatusCode)
		}
	}
	resp, body := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false))
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status %d, want 402 (body %s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("x-semantix-quota-mode"); got != quotaModeBlocked {
		t.Fatalf("mode header %q", got)
	}
	if got := resp.Header.Get("x-semantix-quota-recharge-url"); got != "https://pay.example/topup" {
		t.Fatalf("recharge header %q", got)
	}
	var e apiErrorBody
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("error body %s: %v", body, err)
	}
	if e.Error.Type != "insufficient_quota" || e.Error.Code != "free_tier_exhausted" {
		t.Fatalf("error envelope %+v", e.Error)
	}
	if !strings.Contains(e.Error.Message, "https://pay.example/topup") ||
		!strings.Contains(e.Error.Message, "充值") {
		t.Fatalf("message must carry the recharge URL and 中文提示: %s", e.Error.Message)
	}
}

func TestQuotaBalanceUnlocksPaidModeAndCachesProbe(t *testing.T) {
	upURL, done := billingBase(t)
	defer done()
	var probes atomic.Int64
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer wallet-key" {
			t.Errorf("wallet auth header %q", got)
		}
		_, _ = io.WriteString(w, `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"50.00"}]}`)
	}))
	defer wallet.Close()

	g := newBillingGateway(t, upURL, BillingConfig{
		Enabled: true, FreeTokens: 50, RechargeURL: "https://pay.example/topup",
		BalanceURL: wallet.URL, BalanceKey: "wallet-key", BalanceCacheSeconds: 300,
	})
	srv := httptest.NewServer(g)
	defer srv.Close()

	// exhaust the 50-token tier with one 100-token exchange
	if resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false)); resp.StatusCode != http.StatusOK {
		t.Fatalf("warmup status %d", resp.StatusCode)
	}
	for i := 0; i < 2; i++ {
		resp, body := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("paid request %d: status %d (%s)", i, resp.StatusCode, body)
		}
		if got := resp.Header.Get("x-semantix-quota-mode"); got != quotaModePaid {
			t.Fatalf("paid request %d: mode header %q", i, got)
		}
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("wallet probed %d times, want 1 (cache)", got)
	}
}

func TestQuotaBalanceUnavailableStaysBlocked(t *testing.T) {
	upURL, done := billingBase(t)
	defer done()
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"is_available":false,"balance_infos":[]}`)
	}))
	defer wallet.Close()

	g := newBillingGateway(t, upURL, BillingConfig{
		Enabled: true, FreeTokens: 50, RechargeURL: "https://pay.example/topup",
		BalanceURL: wallet.URL, BalanceKey: "wallet-key",
	})
	srv := httptest.NewServer(g)
	defer srv.Close()

	if resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false)); resp.StatusCode != http.StatusOK {
		t.Fatalf("warmup status %d", resp.StatusCode)
	}
	resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false))
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status %d, want 402 while wallet is empty", resp.StatusCode)
	}
}

func TestQuotaL3ReuseIsFree(t *testing.T) {
	dir := t.TempDir()
	q, err := newQuotaEngine(BillingConfig{Enabled: true, FreeTokens: 1000, RechargeURL: "u"},
		filepath.Join(dir, "quota-state.json"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	g := &Gateway{quota: q}
	g.recordUsage(usage.Event{TokensIn: 500, TokensOut: 100, L3Reuse: true, At: time.Now().Unix()})
	if got := q.Status(context.Background()).UsedTokens; got != 0 {
		t.Fatalf("L3 reuse consumed %d tokens, want 0", got)
	}
	g.recordUsage(usage.Event{TokensIn: 30, TokensOut: 20, At: time.Now().Unix()})
	if got := q.Status(context.Background()).UsedTokens; got != 50 {
		t.Fatalf("forwarded usage consumed %d tokens, want 50", got)
	}
}

func TestQuotaEndpoint(t *testing.T) {
	upURL, done := billingBase(t)
	defer done()
	g := newBillingGateway(t, upURL, BillingConfig{
		Enabled: true, FreeTokens: 1000, RechargeURL: "https://pay.example/topup",
	})
	srv := httptest.NewServer(g)
	defer srv.Close()

	// auth required
	resp, err := http.Get(srv.URL + "/v1/quota")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /v1/quota: %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/quota", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/v1/quota: %d (%s)", resp.StatusCode, body)
	}
	var got struct {
		Object string `json:"object"`
		quotaStatus
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Object != "quota" || got.FreeTokens != 1000 || got.Mode != quotaModeFree ||
		got.RechargeURL != "https://pay.example/topup" {
		t.Fatalf("snapshot %+v", got)
	}
}

func TestQuotaDisabledIsInvisible(t *testing.T) {
	upURL, done := billingBase(t)
	defer done()
	g := newTestGateway(t, upURL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("x-semantix-quota-mode"); got != "" {
		t.Fatalf("quota header %q leaked with billing disabled", got)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/quota", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	qresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	qresp.Body.Close()
	if qresp.StatusCode != http.StatusNotFound {
		t.Fatalf("/v1/quota with billing disabled: %d", qresp.StatusCode)
	}
}

func TestBillingConfigValidation(t *testing.T) {
	base := func() *Config {
		cfg := DefaultConfig()
		cfg.Upstreams = []UpstreamConfig{{
			Name: "up", BaseURL: "http://127.0.0.1:1", APIKey: "k",
			ModelAlias: []string{"m"}, UpstreamModel: "m", Vendor: "deepseek",
		}}
		return cfg
	}

	cfg := base()
	cfg.Billing = BillingConfig{Enabled: true}
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "recharge_url") {
		t.Fatalf("missing recharge_url: %v", err)
	}

	cfg = base()
	cfg.Billing = BillingConfig{Enabled: true, RechargeURL: "https://pay.example", BalanceURL: "https://api.example/user/balance"}
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "balance_url and balance_key") {
		t.Fatalf("balance_url without key: %v", err)
	}

	cfg = base()
	cfg.Billing = BillingConfig{Enabled: true, RechargeURL: "https://pay.example"}
	if err := cfg.validate(); err != nil {
		t.Fatalf("valid billing config: %v", err)
	}
	if cfg.Billing.FreeTokens != defaultFreeTokens {
		t.Fatalf("free_tokens default %d, want %d", cfg.Billing.FreeTokens, defaultFreeTokens)
	}
	if cfg.Billing.BalanceCacheSeconds != 300 {
		t.Fatalf("balance_cache_seconds default %d, want 300", cfg.Billing.BalanceCacheSeconds)
	}

	// disabled billing needs nothing
	cfg = base()
	cfg.Billing = BillingConfig{}
	if err := cfg.validate(); err != nil {
		t.Fatalf("disabled billing: %v", err)
	}
}
