package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/UNICKCHENG/cliproxyapi-plugins/auth-cursor/go/internal/dashboard"
	"github.com/UNICKCHENG/cliproxyapi-plugins/auth-cursor/go/internal/quota"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

type hostAuthListResponse struct {
	Files []pluginapi.HostAuthFileEntry `json:"files"`
}

type cursorAuthSnapshot struct {
	Name      string
	Email     string
	AuthIndex string
	Status    string
	APIKey    string
}

var loadCursorAuths = loadCursorAuthsViaHost

var fetchCursorUsageFn = func(ctx context.Context, apiKey string, forceRefresh bool) (*quota.CursorUsage, error) {
	return quota.DefaultClient.FetchCursorUsageDirect(ctx, apiKey, forceRefresh)
}

func managementRegister() ([]byte, error) {
	return okEnvelope(pluginapi.ManagementRegistrationResponse{
		Routes: []pluginapi.ManagementRoute{{
			Method:      http.MethodGet,
			Path:        "/cursor/usage",
			Description: "Get live usage and quota limits for Cursor credentials",
		}},
		Resources: []pluginapi.ResourceRoute{{
			Path:        "/quota",
			Menu:        "Cursor Quota",
			Description: "Live Cursor Quota and Usage Dashboard",
		}},
	})
}

func managementHandle(raw []byte) ([]byte, error) {
	var req pluginapi.ManagementRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return nil, errUnmarshal
		}
	}
	kind := managementPathKind(req.Path)
	forceRefresh := managementForceRefresh(req.Query)
	accounts, errLoad := cursorQuotaAccounts(forceRefresh)
	if errLoad != nil {
		return okEnvelope(pluginapi.ManagementResponse{
			StatusCode: http.StatusBadGateway,
			Headers:    http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
			Body:       []byte(errLoad.Error()),
		})
	}
	switch kind {
	case "usage":
		body, errMarshal := json.Marshal(map[string]any{
			"ok":         true,
			"accounts":   accounts,
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		})
		if errMarshal != nil {
			return nil, errMarshal
		}
		return okEnvelope(pluginapi.ManagementResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
			Body:       body,
		})
	default:
		html := dashboard.RenderDashboard(dashboard.DashboardData{
			Title:          "Cursor Quota",
			Accounts:       accounts,
			RefreshedAt:    time.Now(),
			RefreshURL:     "?refresh=1",
			APIEndpointURL: "/v0/management/cursor/usage",
		})
		return okEnvelope(pluginapi.ManagementResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       []byte(html),
		})
	}
}

func managementPathKind(path string) string {
	trimmed := strings.ToLower(strings.TrimRight(strings.TrimSpace(path), "/"))
	switch {
	case strings.HasSuffix(trimmed, "/cursor/usage"), strings.HasSuffix(trimmed, "/usage"):
		return "usage"
	default:
		return "quota"
	}
}

func managementForceRefresh(query map[string][]string) bool {
	if query == nil {
		return false
	}
	raw := strings.ToLower(strings.TrimSpace(firstQueryValue(query, "refresh")))
	return raw == "1" || raw == "true" || raw == "yes"
}

func firstQueryValue(query map[string][]string, key string) string {
	if values := query[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func cursorQuotaAccounts(forceRefresh bool) ([]dashboard.AccountView, error) {
	auths, errLoad := loadCursorAuths()
	if errLoad != nil {
		return nil, errLoad
	}
	out := make([]dashboard.AccountView, 0, len(auths))
	for _, auth := range auths {
		view := dashboard.AccountView{
			ID:        auth.Name,
			AuthIndex: auth.AuthIndex,
			Name:      auth.Name,
			Email:     auth.Email,
			UpdatedAt: time.Now(),
		}
		if auth.Status != "" {
			view.StatusMessage = auth.Status
		}
		if auth.APIKey == "" {
			view.Error = "no api_key in auth file"
			out = append(out, view)
			continue
		}
		usage, errUsage := fetchCursorUsageFn(context.Background(), auth.APIKey, forceRefresh)
		if errUsage != nil {
			view.Error = errUsage.Error()
		} else {
			view.Usage = usage
			view.Cached = !forceRefresh
		}
		out = append(out, view)
	}
	return out, nil
}

func loadCursorAuthsViaHost() ([]cursorAuthSnapshot, error) {
	raw, errList := callHost(pluginabi.MethodHostAuthList, map[string]any{})
	if errList != nil {
		return nil, errList
	}
	var listed hostAuthListResponse
	if errUnmarshal := json.Unmarshal(raw, &listed); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	out := make([]cursorAuthSnapshot, 0, len(listed.Files))
	for _, file := range listed.Files {
		if !isCursorAuthEntry(file) {
			continue
		}
		snap := cursorAuthSnapshot{
			Name:      strings.TrimSpace(file.Name),
			Email:     strings.TrimSpace(file.Email),
			AuthIndex: strings.TrimSpace(file.AuthIndex),
			Status:    strings.TrimSpace(file.StatusMessage),
		}
		if snap.AuthIndex == "" {
			snap.AuthIndex = strings.TrimSpace(file.ID)
		}
		if snap.AuthIndex != "" {
			got, errGet := callHost(pluginabi.MethodHostAuthGet, pluginapi.HostAuthGetRequest{AuthIndex: snap.AuthIndex})
			if errGet == nil {
				var resp pluginapi.HostAuthGetResponse
				if errUnmarshal := json.Unmarshal(got, &resp); errUnmarshal == nil {
					snap.APIKey = apiKeyFromStorage(resp.JSON)
					if snap.Name == "" {
						snap.Name = strings.TrimSpace(resp.Name)
					}
					if snap.Email == "" {
						snap.Email = strings.TrimSpace(gjson.GetBytes(resp.JSON, "email").String())
					}
				}
			}
		}
		out = append(out, snap)
	}
	return out, nil
}

func isCursorAuthEntry(file pluginapi.HostAuthFileEntry) bool {
	if strings.EqualFold(strings.TrimSpace(file.Type), providerIdentifier) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(file.Provider), providerIdentifier) {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(file.Name))
	return strings.HasPrefix(name, "cursor-") || strings.HasPrefix(name, "cursor_")
}
