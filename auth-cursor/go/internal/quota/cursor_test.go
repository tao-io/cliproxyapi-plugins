package quota

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCursorUsage_Calculations(t *testing.T) {
	usage := &CursorUsage{
		StartOfMonth:         "2026-09-01T00:00:00.000Z",
		NumRequests:          45,
		NumRequestsTotal:     45,
		MaxRequestUsage:      500,
		MaxRequestUsageTotal: 500,
	}

	pct := usage.UsagePercentage()
	if pct != 9.0 {
		t.Fatalf("UsagePercentage() = %f, want 9.0", pct)
	}

	msg := usage.FormatStatusMessage()
	if !strings.HasPrefix(msg, "Usage: 45 / 500 fast requests (resets in ") {
		t.Fatalf("FormatStatusMessage() = %q, unexpected prefix", msg)
	}
	if !strings.HasSuffix(msg, "days)") && !strings.HasSuffix(msg, "day)") {
		t.Fatalf("FormatStatusMessage() = %q, unexpected suffix", msg)
	}

	reset := usage.ResetDate()
	if reset.IsZero() {
		t.Fatal("ResetDate() should not be zero")
	}
	if !reset.After(time.Now()) {
		t.Fatalf("ResetDate() = %v should be in the future", reset)
	}
}

func TestCursorUsage_PlanUsage(t *testing.T) {
	usage := &CursorUsage{
		Plan:               true,
		APIPercentUsed:     100,
		AutoPercentUsed:    73.96,
		TotalPercentUsed:   75.21,
		LimitCents:         2000,
		IncludedSpendCents: 2000,
		CycleStart:         time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC),
		CycleEnd:           time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		DisplayMessage:     "You've hit your usage limit",
	}
	if got := usage.UsagePercentage(); got != 75.21 {
		t.Fatalf("UsagePercentage() = %f, want 75.21", got)
	}
	if got := usage.HeadlinePercent(); got != 100 {
		t.Fatalf("HeadlinePercent() = %f, want 100", got)
	}
	if !usage.IncludedExhausted() {
		t.Fatal("expected included spend exhausted")
	}
	if got := usage.FormatIncludedSpend(); got != "$20.00 / $20.00" {
		t.Fatalf("FormatIncludedSpend() = %q", got)
	}
	msg := usage.FormatStatusMessage()
	if !strings.Contains(msg, "100.0% API") || !strings.Contains(msg, "74.0% Auto") || !strings.Contains(msg, "Resets 23 Sep") {
		t.Fatalf("FormatStatusMessage() = %q", msg)
	}
	if !usage.ResetDate().Equal(usage.CycleEnd) {
		t.Fatalf("ResetDate() = %v, want CycleEnd", usage.ResetDate())
	}
	now := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	if got := usage.FormatResetIn(now); got != "1d 14h" {
		t.Fatalf("FormatResetIn() = %q, want 1d 14h", got)
	}
}

func TestDecodePeriodUsage_PlanJSON(t *testing.T) {
	body := []byte(`{
		"billingCycleStart": 1787446379000,
		"billingCycleEnd": "1790124779000",
		"displayMessage": "You've hit your usage limit",
		"planUsage": {
			"totalSpend": 2000,
			"includedSpend": 2000,
			"remaining": 0,
			"limit": 2000,
			"autoPercentUsed": 73.96,
			"apiPercentUsed": 100,
			"totalPercentUsed": 75.21
		}
	}`)
	usage, err := decodePeriodUsage(body)
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Plan || usage.APIPercentUsed != 100 || usage.AutoPercentUsed != 73.96 {
		t.Fatalf("unexpected plan fields: %+v", usage)
	}
	if usage.LimitCents != 2000 || usage.IncludedSpendCents != 2000 {
		t.Fatalf("unexpected cents: %+v", usage)
	}
	if usage.TotalSpendCents != 2000 {
		t.Fatalf("TotalSpendCents = %d", usage.TotalSpendCents)
	}
	if usage.CycleStart.IsZero() || usage.CycleEnd.IsZero() {
		t.Fatalf("expected cycle bounds, got start=%v end=%v", usage.CycleStart, usage.CycleEnd)
	}
	if usage.StartOfMonth == "" {
		t.Fatal("StartOfMonth should be filled from billingCycleStart")
	}
}

func TestDecodePeriodUsage_LegacyJSON(t *testing.T) {
	body := []byte(`{
		"startOfMonth": "2026-09-01T00:00:00.000Z",
		"numRequests": 50,
		"numRequestsTotal": 50,
		"maxRequestUsage": 500,
		"maxRequestUsageTotal": 500
	}`)
	usage, err := decodePeriodUsage(body)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan {
		t.Fatal("legacy payload must not set Plan")
	}
	if usage.NumRequests != 50 || usage.MaxRequestUsage != 500 {
		t.Fatalf("unexpected legacy fields: %+v", usage)
	}
}

func TestFetchCursorUsage_SuccessAndCache(t *testing.T) {
	var exchangeHits, usageHits atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/exchange_user_api_key", func(w http.ResponseWriter, r *http.Request) {
		exchangeHits.Add(1)
		if r.Header.Get("Authorization") != "Bearer crsr_test_token" {
			t.Errorf("unexpected exchange Authorization: %s", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": "jwt_test"})
	})
	mux.HandleFunc("/usage", func(w http.ResponseWriter, r *http.Request) {
		usageHits.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer jwt_test" {
			t.Errorf("unexpected usage Authorization: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("connect-protocol-version") != "1" {
			t.Errorf("unexpected connect-protocol-version: %s", r.Header.Get("connect-protocol-version"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"planUsage": map[string]any{
				"apiPercentUsed":   12.5,
				"autoPercentUsed":  4.0,
				"totalPercentUsed": 5.0,
				"limit":            2000,
				"includedSpend":    250,
			},
			"billingCycleStart": 1787446379000,
			"billingCycleEnd":   1790124779000,
			"displayMessage":    "You've used 12% of your included usage",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(
		WithEndpoint(server.URL+"/usage"),
		WithExchangeEndpoint(server.URL+"/auth/exchange_user_api_key"),
		WithHTTPClient(server.Client()),
		WithCacheTTL(5*time.Minute),
	)

	ctx := context.Background()

	usage1, err := client.FetchCursorUsage(ctx, "crsr_test_token")
	if err != nil {
		t.Fatalf("FetchCursorUsage failed: %v", err)
	}
	if !usage1.Plan || usage1.APIPercentUsed != 12.5 || usage1.LimitCents != 2000 {
		t.Fatalf("unexpected usage1: %+v", usage1)
	}
	if exchangeHits.Load() != 1 || usageHits.Load() != 1 {
		t.Fatalf("hits exchange=%d usage=%d, want 1/1", exchangeHits.Load(), usageHits.Load())
	}

	usage2, err := client.FetchCursorUsage(ctx, "crsr_test_token")
	if err != nil {
		t.Fatalf("FetchCursorUsage call 2 failed: %v", err)
	}
	if usage2.APIPercentUsed != 12.5 {
		t.Fatalf("unexpected usage2: %+v", usage2)
	}
	if exchangeHits.Load() != 1 || usageHits.Load() != 1 {
		t.Fatalf("cache should reuse both JWT and usage, exchange=%d usage=%d", exchangeHits.Load(), usageHits.Load())
	}

	usage3, err := client.FetchCursorUsageDirect(ctx, "crsr_test_token", true)
	if err != nil {
		t.Fatalf("FetchCursorUsageDirect failed: %v", err)
	}
	if usage3.APIPercentUsed != 12.5 {
		t.Fatalf("unexpected usage3: %+v", usage3)
	}
	if exchangeHits.Load() != 1 {
		t.Fatalf("forceRefresh should reuse JWT, exchange=%d", exchangeHits.Load())
	}
	if usageHits.Load() != 2 {
		t.Fatalf("hitCount after forceRefresh = %d, want 2", usageHits.Load())
	}
}

func TestFetchCursorUsage_Unauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/exchange_user_api_key", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthenticated"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(
		WithEndpoint(server.URL+"/usage"),
		WithExchangeEndpoint(server.URL+"/auth/exchange_user_api_key"),
		WithHTTPClient(server.Client()),
	)

	_, err := client.FetchCursorUsage(context.Background(), "invalid_key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got: %v", err)
	}
}

func TestFetchCursorUsage_Usage401RetriesExchange(t *testing.T) {
	var exchangeHits, usageHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/exchange_user_api_key", func(w http.ResponseWriter, r *http.Request) {
		n := exchangeHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken": "jwt_" + strings.Repeat("x", int(n)),
		})
	})
	mux.HandleFunc("/usage", func(w http.ResponseWriter, r *http.Request) {
		n := usageHits.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"planUsage": map[string]any{"apiPercentUsed": 1.0, "totalPercentUsed": 1.0, "limit": 2000},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(
		WithEndpoint(server.URL+"/usage"),
		WithExchangeEndpoint(server.URL+"/auth/exchange_user_api_key"),
		WithHTTPClient(server.Client()),
	)
	usage, err := client.FetchCursorUsage(context.Background(), "crsr_retry")
	if err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}
	if !usage.Plan || usage.APIPercentUsed != 1.0 {
		t.Fatalf("unexpected usage after retry: %+v", usage)
	}
	if exchangeHits.Load() != 2 || usageHits.Load() != 2 {
		t.Fatalf("hits exchange=%d usage=%d, want 2/2", exchangeHits.Load(), usageHits.Load())
	}
}

func TestFetchCursorUsage_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	client := NewClient(
		WithEndpoint(server.URL),
		WithExchangeEndpoint(""),
		WithHTTPClient(server.Client()),
	)

	_, err := client.FetchCursorUsage(ctx, "crsr_timeout_test")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestFetchCursorUsage_EmptyKey(t *testing.T) {
	client := NewClient()
	_, err := client.FetchCursorUsage(context.Background(), "   ")
	if !errors.Is(err, ErrEmptyAPIKey) {
		t.Fatalf("expected ErrEmptyAPIKey, got: %v", err)
	}
}

func TestLooksLikeJWT(t *testing.T) {
	if looksLikeJWT("crsr_abc.def.ghi") {
		t.Fatal("crsr_ keys must not be treated as JWTs")
	}
	if !looksLikeJWT("aaa.bbb.ccc") {
		t.Fatal("three-part token should look like a JWT")
	}
}
