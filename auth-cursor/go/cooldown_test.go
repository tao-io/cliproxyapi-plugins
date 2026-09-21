package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/UNICKCHENG/cliproxyapi-plugins/auth-cursor/go/internal/quota"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

func TestIsAdditionalPoolModel(t *testing.T) {
	for _, id := range []string{"muse-spark-1.3", "gpt-5.6-luna", "claude-opus-5", "gemini-3.7-flash", "kimi-k2"} {
		if !isAdditionalPoolModel(id) {
			t.Errorf("%s should be in the additional pool", id)
		}
	}
	for _, id := range []string{"composer-1.5", "grok-4.6", "auto", "auto-smart", "default"} {
		if isAdditionalPoolModel(id) {
			t.Errorf("%s should stay on the included pool", id)
		}
	}
}

func TestAdditionalPoolCooledUntil(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	storage := []byte(`{"type":"cursor","api_key":"k","additional_pool_cooled_until":"2026-09-23T00:00:00Z"}`)
	until, ok := additionalPoolCooledUntil(storage, now)
	if !ok || until != time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("until=%v ok=%t", until, ok)
	}
	_, expired := additionalPoolCooledUntil(storage, time.Date(2026, 9, 23, 0, 0, 1, 0, time.UTC))
	if expired {
		t.Fatal("cooldown must lift once the reset instant has passed")
	}
}

func TestStorageWithAdditionalPoolCooldownRoundTrip(t *testing.T) {
	until := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	patched, errPatch := storageWithAdditionalPoolCooldown([]byte(`{"type":"cursor","api_key":"k","email":"a@b.c"}`), until)
	if errPatch != nil {
		t.Fatal(errPatch)
	}
	if got := gjson.GetBytes(patched, "api_key").String(); got != "k" {
		t.Fatalf("api_key = %q", got)
	}
	got, ok := additionalPoolCooledUntil(patched, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if !ok || !got.Equal(until) {
		t.Fatalf("until=%v ok=%t", got, ok)
	}
}

func TestExcludedModelsForRequestAppendsAdditionalPoolWhileCooled(t *testing.T) {
	storage := []byte(`{"type":"cursor","api_key":"good-key","additional_pool_cooled_until":"2026-09-23T00:00:00Z"}`)
	patterns := excludedModelsForRequest(pluginapi.HostConfigSummary{}, nil, storage)
	found := false
	for _, pattern := range patterns {
		if pattern == "muse*" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("patterns = %v, want muse* while additional pool is cooled", patterns)
	}
}

func TestAuthFileName(t *testing.T) {
	if got := authFileName("cursor-dread9ko@gmail.com.json", nil); got != "cursor-dread9ko@gmail.com.json" {
		t.Fatalf("from auth id: %q", got)
	}
	if got := authFileName("auth-index-9", []byte(`{"email":"info@fridmanproperties.com"}`)); got != "cursor-info@fridmanproperties.com.json" {
		t.Fatalf("from email: %q", got)
	}
}

func TestPersistAdditionalPoolCooldownWritesAuthFile(t *testing.T) {
	var savedName string
	var savedJSON json.RawMessage
	orig := saveAuthFile
	saveAuthFile = func(name string, raw json.RawMessage) error {
		savedName = name
		savedJSON = append(json.RawMessage(nil), raw...)
		return nil
	}
	t.Cleanup(func() { saveAuthFile = orig })

	until := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	persistAdditionalPoolCooldown("cursor-dread9ko@gmail.com.json", []byte(`{"type":"cursor","api_key":"k"}`), until)
	if savedName != "cursor-dread9ko@gmail.com.json" {
		t.Fatalf("saved name = %q", savedName)
	}
	got, ok := additionalPoolCooledUntil(savedJSON, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if !ok || !got.Equal(until) {
		t.Fatalf("saved until=%v ok=%t json=%s", got, ok, savedJSON)
	}
}

func TestExecuteShortCircuitsCooledAdditionalModel(t *testing.T) {
	storage := []byte(`{"type":"cursor","api_key":"good-key","additional_pool_cooled_until":"2026-09-23T00:00:00Z"}`)
	payload, _ := json.Marshal(map[string]any{
		"model":    "muse-spark-1.3",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	raw, errMarshal := json.Marshal(rpcExecutorRequest{
		ExecutorRequest: pluginapi.ExecutorRequest{
			AuthID:      "cursor-dread9ko@gmail.com.json",
			Model:       "muse-spark-1.3",
			Payload:     payload,
			StorageJSON: storage,
		},
	})
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	resp, errExecute := execute(raw)
	if errExecute != nil {
		t.Fatalf("execute: %v", errExecute)
	}
	env := decodeEnvelope(t, resp)
	if env.OK {
		t.Fatal("cooled additional-pool request must fail over, not succeed")
	}
	if env.Error == nil || env.Error.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("error = %+v, want 429", env.Error)
	}
	if !env.Error.Retryable {
		t.Fatal("retryable = false, want true so the host failovers")
	}
}

func TestExecuteDoesNotBlockIncludedPoolWhileAdditionalCooled(t *testing.T) {
	useFakeBridge(t)
	storage := []byte(`{"type":"cursor","api_key":"good-key","additional_pool_cooled_until":"2026-09-23T00:00:00Z"}`)
	payload, _ := json.Marshal(map[string]any{
		"model":    "fake-model",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	raw, errMarshal := json.Marshal(rpcExecutorRequest{
		ExecutorRequest: pluginapi.ExecutorRequest{
			Model:       "fake-model",
			Payload:     payload,
			StorageJSON: storage,
		},
	})
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	resp, errExecute := execute(raw)
	if errExecute != nil {
		t.Fatalf("execute: %v", errExecute)
	}
	env := decodeEnvelope(t, resp)
	if !env.OK {
		t.Fatalf("included-pool model must still run: %+v", env.Error)
	}
}

func TestRememberUsageLimitPersistsOnMuse(t *testing.T) {
	var saved bool
	orig := saveAuthFile
	saveAuthFile = func(name string, raw json.RawMessage) error {
		saved = true
		if gjson.GetBytes(raw, additionalPoolCooledUntilField).String() != "2026-09-23T00:00:00Z" {
			t.Fatalf("saved json = %s", raw)
		}
		return nil
	}
	t.Cleanup(func() { saveAuthFile = orig })

	failure := runFailure("You've hit your usage limit · you saved $312 on API model usage. Usage will reset when your monthly cycle ends on 9/23/2026.")
	rememberUsageLimit(pluginapi.ExecutorRequest{
		AuthID:      "cursor-dread9ko@gmail.com.json",
		StorageJSON: []byte(`{"type":"cursor","api_key":"k"}`),
	}, "muse-spark-1.3", failure)
	if !saved {
		t.Fatal("usage-limit on Muse must persist additional_pool_cooled_until")
	}
}

func TestManagementRegisterAdvertisesQuotaPage(t *testing.T) {
	raw, errHandle := handleMethod(pluginabi.MethodManagementRegister, nil)
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK {
		t.Fatalf("%+v", env.Error)
	}
	if got := gjson.GetBytes(env.Result, "Resources.0.Path").String(); got != "/quota" {
		t.Fatalf("resource path = %q", got)
	}
	if got := gjson.GetBytes(env.Result, "Resources.0.Menu").String(); got != "Cursor Quota" {
		t.Fatalf("menu = %q", got)
	}
}

func TestManagementHandleRendersQuotaHTML(t *testing.T) {
	origLoad := loadCursorAuths
	origFetch := fetchCursorUsageFn
	loadCursorAuths = func() ([]cursorAuthSnapshot, error) {
		return []cursorAuthSnapshot{{
			Name:   "cursor-dev.json",
			Email:  "dev@example.com",
			APIKey: "crsr_test",
		}}, nil
	}
	fetchCursorUsageFn = func(_ context.Context, _ string, _ bool) (*quota.CursorUsage, error) {
		return &quota.CursorUsage{
			StartOfMonth:         "2026-09-01T00:00:00.000Z",
			NumRequests:          45,
			NumRequestsTotal:     45,
			MaxRequestUsage:      500,
			MaxRequestUsageTotal: 500,
		}, nil
	}
	t.Cleanup(func() {
		loadCursorAuths = origLoad
		fetchCursorUsageFn = origFetch
	})

	rawReq, _ := json.Marshal(pluginapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/resource/plugins/auth-cursor/quota",
	})
	raw, errHandle := handleMethod(pluginabi.MethodManagementHandle, rawReq)
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK {
		t.Fatalf("%+v", env.Error)
	}
	var resp pluginapi.ManagementResponse
	if errUnmarshal := json.Unmarshal(env.Result, &resp); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := string(resp.Body)
	if !strings.Contains(body, "dev@example.com") {
		t.Fatalf("html missing email: %s", body)
	}
	if !strings.Contains(body, "color-scheme") {
		t.Fatal("html must declare color-scheme so the iframe is not white")
	}
}
