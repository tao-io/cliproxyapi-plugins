package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

// additionalPoolCooledUntilField is persisted on the Cursor auth JSON. It hides the
// extra-model pool (Muse, leftover GPT/Claude/Gemini/Kimi ids) for this credential until
// the parsed reset instant. Composer, Grok and Auto stay eligible.
const additionalPoolCooledUntilField = "additional_pool_cooled_until"

// additionalPoolPatterns match Cursor ids that share the paid API/extra-model pool.
var additionalPoolPatterns = []string{
	"muse*",
	"gpt-*",
	"claude-*",
	"gemini-*",
	"kimi-k2*",
}

// additionalPoolNow is replaced in tests.
var additionalPoolNow = time.Now

// saveAuthFile persists credential JSON through the host. Tests replace it.
var saveAuthFile = saveAuthFileViaHost

func saveAuthFileViaHost(name string, raw json.RawMessage) error {
	_, errCall := callHost(pluginabi.MethodHostAuthSave, pluginapi.HostAuthSaveRequest{
		Name: name,
		JSON: raw,
	})
	return errCall
}

func isAdditionalPoolModel(modelID string) bool {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" {
		return false
	}
	for _, pattern := range additionalPoolPatterns {
		if matchExcludedModel(pattern, id) {
			return true
		}
	}
	return false
}

func additionalPoolCooledUntil(storage []byte, now time.Time) (time.Time, bool) {
	if len(storage) == 0 || !gjson.ValidBytes(storage) {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(gjson.GetBytes(storage, additionalPoolCooledUntilField).String())
	if raw == "" {
		return time.Time{}, false
	}
	until, errParse := time.Parse(time.RFC3339, raw)
	if errParse != nil {
		until, errParse = time.Parse(time.RFC3339Nano, raw)
	}
	if errParse != nil {
		return time.Time{}, false
	}
	until = until.UTC()
	if !until.After(now.UTC()) {
		return time.Time{}, false
	}
	return until, true
}

func storageWithAdditionalPoolCooldown(storage []byte, until time.Time) ([]byte, error) {
	var obj map[string]any
	if errUnmarshal := json.Unmarshal(storage, &obj); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if obj == nil {
		obj = map[string]any{}
	}
	obj[additionalPoolCooledUntilField] = until.UTC().Format(time.RFC3339)
	return json.Marshal(obj)
}

func authFileName(authID string, storage []byte) string {
	if base := filepath.Base(strings.TrimSpace(authID)); strings.HasSuffix(strings.ToLower(base), ".json") {
		return base
	}
	email := strings.TrimSpace(gjson.GetBytes(storage, "email").String())
	if email != "" {
		return "cursor-" + email + ".json"
	}
	return ""
}

func persistAdditionalPoolCooldown(authID string, storage []byte, until time.Time) {
	if until.IsZero() || len(storage) == 0 {
		return
	}
	if existing, ok := additionalPoolCooledUntil(storage, additionalPoolNow()); ok && !existing.Before(until.UTC()) {
		return
	}
	name := authFileName(authID, storage)
	if name == "" {
		hostLog("warn", "cursor additional-pool cooldown not persisted: missing auth file name", map[string]any{
			"until": until.UTC().Format(time.RFC3339),
		})
		return
	}
	patched, errPatch := storageWithAdditionalPoolCooldown(storage, until)
	if errPatch != nil {
		hostLog("warn", "cursor additional-pool cooldown not persisted: invalid auth json", map[string]any{
			"auth_file": name,
			"reason":    errPatch.Error(),
		})
		return
	}
	if errSave := saveAuthFile(name, patched); errSave != nil {
		hostLog("warn", "cursor additional-pool cooldown not persisted", map[string]any{
			"auth_file": name,
			"reason":    errSave.Error(),
		})
		return
	}
	hostLog("info", "cursor additional-pool cooled until reset", map[string]any{
		"auth_file": name,
		"until":     until.UTC().Format(time.RFC3339),
	})
}

func additionalPoolBlocked(req pluginapi.ExecutorRequest, model string) (upstreamFailure, bool) {
	if !isAdditionalPoolModel(model) {
		return upstreamFailure{}, false
	}
	until, ok := additionalPoolCooledUntil(req.StorageJSON, additionalPoolNow())
	if !ok {
		return upstreamFailure{}, false
	}
	return usageLimitFailure("cursor additional-model pool is cooling down until "+until.Format(time.RFC3339), until), true
}

func rememberUsageLimit(req pluginapi.ExecutorRequest, model string, failure upstreamFailure) {
	until := failure.AdditionalPoolUntil
	if until.IsZero() {
		parsed, ok := usageLimitResetAt(failure.Message)
		if !ok {
			return
		}
		until = parsed
	}
	if !isAdditionalPoolModel(model) && !strings.Contains(strings.ToLower(failure.Message), "api model usage") {
		return
	}
	persistAdditionalPoolCooldown(req.AuthID, req.StorageJSON, until)
}
