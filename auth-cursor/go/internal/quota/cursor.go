package quota

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultCursorQuotaEndpoint is the Connect-RPC endpoint for Cursor usage.
	DefaultCursorQuotaEndpoint = "https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage"
	// DefaultCursorExchangeEndpoint trades a crsr_ API key for a short-lived Dashboard JWT.
	DefaultCursorExchangeEndpoint = "https://api2.cursor.sh/auth/exchange_user_api_key"
	// DefaultTimeout is the strict HTTP timeout for querying Cursor.
	DefaultTimeout = 10 * time.Second
	// DefaultCacheTTL is the memory cache duration for quota queries.
	DefaultCacheTTL = 10 * time.Minute
	// tokenRefreshSkew is how early a cached JWT is treated as expired.
	tokenRefreshSkew = 5 * time.Minute
	// tokenFallbackTTL is used when the JWT payload has no readable exp.
	tokenFallbackTTL = 55 * time.Minute
)

var (
	ErrEmptyAPIKey        = errors.New("cursor api key is required")
	ErrUnauthorized       = errors.New("cursor api unauthorized (invalid or expired key)")
	ErrServiceUnavailable = errors.New("cursor api unavailable")
)

// CursorUsage represents the quota response from Cursor's DashboardService.
type CursorUsage struct {
	StartOfMonth         string    `json:"startOfMonth"`
	NumRequests          int       `json:"numRequests"`
	NumRequestsTotal     int       `json:"numRequestsTotal"`
	MaxRequestUsage      int       `json:"maxRequestUsage"`
	MaxRequestUsageTotal int       `json:"maxRequestUsageTotal"`
	CachedAt             time.Time `json:"cachedAt,omitempty"`

	// Plan is set when GetCurrentPeriodUsage returned the current planUsage object.
	Plan               bool      `json:"plan,omitempty"`
	APIPercentUsed     float64   `json:"apiPercentUsed,omitempty"`
	AutoPercentUsed    float64   `json:"autoPercentUsed,omitempty"`
	TotalPercentUsed   float64   `json:"totalPercentUsed,omitempty"`
	LimitCents         int       `json:"limitCents,omitempty"`
	IncludedSpendCents int       `json:"includedSpendCents,omitempty"`
	RemainingCents     int       `json:"remainingCents,omitempty"`
	TotalSpendCents    int       `json:"totalSpendCents,omitempty"`
	CycleStart         time.Time `json:"cycleStart,omitempty"`
	CycleEnd           time.Time `json:"cycleEnd,omitempty"`
	DisplayMessage     string    `json:"displayMessage,omitempty"`
}

// ResetDate is the next billing-cycle reset. Plan responses carry CycleEnd; the
// legacy request-count payload is inferred from StartOfMonth + 1 month.
func (u *CursorUsage) ResetDate() time.Time {
	if u == nil {
		return time.Time{}
	}
	if !u.CycleEnd.IsZero() {
		return u.CycleEnd.UTC()
	}
	if strings.TrimSpace(u.StartOfMonth) == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, u.StartOfMonth)
	if err != nil {
		t, err = time.Parse(time.RFC3339, u.StartOfMonth)
		if err != nil {
			return time.Time{}
		}
	}
	reset := t.AddDate(0, 1, 0)
	now := time.Now()
	for !reset.After(now) {
		reset = reset.AddDate(0, 1, 0)
	}
	return reset
}

// DaysRemaining returns the number of days until the billing cycle resets.
func (u *CursorUsage) DaysRemaining() int {
	reset := u.ResetDate()
	if reset.IsZero() {
		return 0
	}
	remaining := time.Until(reset)
	days := int(math.Ceil(remaining.Hours() / 24.0))
	if days < 0 {
		return 0
	}
	return days
}

// UsagePercentage is the headline plan figure: total percent for planUsage,
// otherwise the legacy fast-request ratio.
func (u *CursorUsage) UsagePercentage() float64 {
	if u == nil {
		return 0
	}
	if u.Plan {
		if u.TotalPercentUsed > 0 {
			return clampPercent(u.TotalPercentUsed)
		}
		return u.HeadlinePercent()
	}
	if u.MaxRequestUsage <= 0 {
		return 0
	}
	return clampPercent((float64(u.NumRequests) / float64(u.MaxRequestUsage)) * 100.0)
}

// HeadlinePercent is the worst of API / Auto / total, used for card badges.
func (u *CursorUsage) HeadlinePercent() float64 {
	if u == nil {
		return 0
	}
	if u.Plan {
		return math.Max(clampPercent(u.APIPercentUsed), math.Max(clampPercent(u.AutoPercentUsed), clampPercent(u.TotalPercentUsed)))
	}
	return u.UsagePercentage()
}

// IncludedExhausted is true when included spend has reached the plan limit.
func (u *CursorUsage) IncludedExhausted() bool {
	return u != nil && u.Plan && u.LimitCents > 0 && u.IncludedSpendCents >= u.LimitCents
}

// FormatStatusMessage formats the human-readable summary for credential cards.
func (u *CursorUsage) FormatStatusMessage() string {
	if u == nil {
		return ""
	}
	if u.Plan {
		reset := u.ResetDate()
		resetStr := "unknown"
		if !reset.IsZero() {
			resetStr = reset.Format("02 Jan")
		}
		return fmt.Sprintf("Usage: %.1f%% API (%.1f%% Auto) | Resets %s", u.APIPercentUsed, u.AutoPercentUsed, resetStr)
	}
	days := u.DaysRemaining()
	dayStr := "days"
	if days == 1 {
		dayStr = "day"
	}
	if u.MaxRequestUsage > 0 {
		return fmt.Sprintf("Usage: %d / %d fast requests (resets in %d %s)", u.NumRequests, u.MaxRequestUsage, days, dayStr)
	}
	return fmt.Sprintf("Usage: %d fast requests (resets in %d %s)", u.NumRequests, days, dayStr)
}

// FormatIncludedSpend renders included spend against the plan limit.
func (u *CursorUsage) FormatIncludedSpend() string {
	if u == nil || !u.Plan || u.LimitCents <= 0 {
		return ""
	}
	return fmt.Sprintf("%s / %s", formatCents(u.IncludedSpendCents), formatCents(u.LimitCents))
}

func formatCents(cents int) string {
	return fmt.Sprintf("$%.2f", float64(cents)/100.0)
}

// FormatResetIn is the Cursor dashboard reset label, e.g. "1d 23h".
func (u *CursorUsage) FormatResetIn(now time.Time) string {
	if u == nil {
		return ""
	}
	reset := u.ResetDate()
	if reset.IsZero() {
		return ""
	}
	if now.IsZero() {
		now = time.Now()
	}
	d := reset.Sub(now)
	if d <= 0 {
		return "now"
	}
	hours := int(d.Hours())
	days := hours / 24
	hours = hours % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	mins := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func clampPercent(pct float64) float64 {
	if pct < 0 {
		return 0
	}
	return pct
}

type cacheEntry struct {
	usage     *CursorUsage
	expiresAt time.Time
}

type tokenEntry struct {
	token     string
	expiresAt time.Time
}

// Client queries Cursor usage metrics with built-in caching and timeout control.
type Client struct {
	endpoint         string
	exchangeEndpoint string
	httpClient       *http.Client
	ttl              time.Duration
	cacheMu          sync.RWMutex
	cache            map[string]cacheEntry
	tokenMu          sync.Mutex
	tokens           map[string]tokenEntry
}

// Option configures Client instances.
type Option func(*Client)

// WithEndpoint overrides the default Connect-RPC endpoint.
func WithEndpoint(endpoint string) Option {
	return func(c *Client) {
		if strings.TrimSpace(endpoint) != "" {
			c.endpoint = strings.TrimSpace(endpoint)
		}
	}
}

// WithExchangeEndpoint overrides the API-key exchange URL. An empty value
// skips exchange and sends the stored credential as the Bearer token.
func WithExchangeEndpoint(endpoint string) Option {
	return func(c *Client) {
		c.exchangeEndpoint = strings.TrimSpace(endpoint)
	}
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithCacheTTL overrides the in-memory cache TTL.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) {
		if ttl > 0 {
			c.ttl = ttl
		}
	}
}

// NewClient creates a new Cursor quota Client.
func NewClient(opts ...Option) *Client {
	endpoint := DefaultCursorQuotaEndpoint
	if env := os.Getenv("CURSOR_QUOTA_ENDPOINT"); env != "" {
		endpoint = env
	}
	exchange := DefaultCursorExchangeEndpoint
	if env := os.Getenv("CURSOR_EXCHANGE_ENDPOINT"); env != "" {
		exchange = env
	}
	c := &Client{
		endpoint:         endpoint,
		exchangeEndpoint: exchange,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		ttl:    DefaultCacheTTL,
		cache:  make(map[string]cacheEntry),
		tokens: make(map[string]tokenEntry),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DefaultClient is the package-level quota Client.
var DefaultClient = NewClient()

// FetchCursorUsage queries Cursor usage for apiKey with caching.
func FetchCursorUsage(ctx context.Context, apiKey string) (*CursorUsage, error) {
	return DefaultClient.FetchCursorUsage(ctx, apiKey)
}

// FetchCursorUsage queries Cursor usage using the Client's configuration and cache.
func (c *Client) FetchCursorUsage(ctx context.Context, apiKey string) (*CursorUsage, error) {
	return c.FetchCursorUsageDirect(ctx, apiKey, false)
}

// FetchCursorUsageDirect queries Cursor usage, optionally bypassing cache.
func (c *Client) FetchCursorUsageDirect(ctx context.Context, apiKey string, forceRefresh bool) (*CursorUsage, error) {
	trimmedKey := strings.TrimSpace(apiKey)
	if trimmedKey == "" {
		return nil, ErrEmptyAPIKey
	}

	cacheKey := hashKey(trimmedKey)
	now := time.Now()

	if !forceRefresh {
		c.cacheMu.RLock()
		entry, ok := c.cache[cacheKey]
		c.cacheMu.RUnlock()
		if ok && now.Before(entry.expiresAt) && entry.usage != nil {
			return entry.usage, nil
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	usage, errFetch := c.fetchOnce(reqCtx, trimmedKey)
	if errors.Is(errFetch, ErrUnauthorized) && c.shouldExchange(trimmedKey) {
		c.forgetToken(trimmedKey)
		usage, errFetch = c.fetchOnce(reqCtx, trimmedKey)
	}
	if errFetch != nil {
		return nil, errFetch
	}
	usage.CachedAt = now

	c.cacheMu.Lock()
	c.cache[cacheKey] = cacheEntry{
		usage:     usage,
		expiresAt: now.Add(c.ttl),
	}
	c.cacheMu.Unlock()

	return usage, nil
}

func (c *Client) fetchOnce(ctx context.Context, apiKey string) (*CursorUsage, error) {
	token, errToken := c.resolveAccessToken(ctx, apiKey)
	if errToken != nil {
		return nil, errToken
	}

	httpReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader([]byte("{}")))
	if errReq != nil {
		return nil, fmt.Errorf("create request: %w", errReq)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("connect-protocol-version", "1")

	resp, errDo := c.httpClient.Do(httpReq)
	if errDo != nil {
		if errors.Is(errDo, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("cursor request timed out after %s: %w", DefaultTimeout, errDo)
		}
		return nil, fmt.Errorf("cursor request failed: %w", errDo)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return nil, fmt.Errorf("read cursor response body: %w", errRead)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: status %d: %s", ErrServiceUnavailable, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	usage, errDecode := decodePeriodUsage(body)
	if errDecode != nil {
		return nil, errDecode
	}
	return usage, nil
}

func (c *Client) shouldExchange(apiKey string) bool {
	return c.exchangeEndpoint != "" && !looksLikeJWT(apiKey)
}

func (c *Client) resolveAccessToken(ctx context.Context, apiKey string) (string, error) {
	if !c.shouldExchange(apiKey) {
		return apiKey, nil
	}
	cacheKey := hashKey(apiKey)
	now := time.Now()
	c.tokenMu.Lock()
	entry, ok := c.tokens[cacheKey]
	c.tokenMu.Unlock()
	if ok && now.Before(entry.expiresAt) && entry.token != "" {
		return entry.token, nil
	}

	token, errExchange := c.exchangeAPIKey(ctx, apiKey)
	if errExchange != nil {
		return "", errExchange
	}
	c.tokenMu.Lock()
	c.tokens[cacheKey] = tokenEntry{
		token:     token,
		expiresAt: tokenExpiry(token),
	}
	c.tokenMu.Unlock()
	return token, nil
}

func (c *Client) forgetToken(apiKey string) {
	c.tokenMu.Lock()
	delete(c.tokens, hashKey(apiKey))
	c.tokenMu.Unlock()
}

type exchangeResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func (c *Client) exchangeAPIKey(ctx context.Context, apiKey string) (string, error) {
	httpReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, c.exchangeEndpoint, bytes.NewReader([]byte("{}")))
	if errReq != nil {
		return "", fmt.Errorf("create exchange request: %w", errReq)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, errDo := c.httpClient.Do(httpReq)
	if errDo != nil {
		if errors.Is(errDo, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("cursor exchange timed out after %s: %w", DefaultTimeout, errDo)
		}
		return "", fmt.Errorf("cursor exchange failed: %w", errDo)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return "", fmt.Errorf("read cursor exchange body: %w", errRead)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: exchange status %d: %s", ErrServiceUnavailable, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed exchangeResponse
	if errUnmarshal := json.Unmarshal(body, &parsed); errUnmarshal != nil {
		return "", fmt.Errorf("decode cursor exchange response: %w", errUnmarshal)
	}
	token := strings.TrimSpace(parsed.AccessToken)
	if token == "" {
		return "", fmt.Errorf("%w: exchange returned no access token", ErrServiceUnavailable)
	}
	return token, nil
}

type planUsageJSON struct {
	TotalSpend       int     `json:"totalSpend"`
	IncludedSpend    int     `json:"includedSpend"`
	Remaining        int     `json:"remaining"`
	Limit            int     `json:"limit"`
	AutoPercentUsed  float64 `json:"autoPercentUsed"`
	APIPercentUsed   float64 `json:"apiPercentUsed"`
	TotalPercentUsed float64 `json:"totalPercentUsed"`
}

type periodUsageJSON struct {
	StartOfMonth         string          `json:"startOfMonth"`
	NumRequests          int             `json:"numRequests"`
	NumRequestsTotal     int             `json:"numRequestsTotal"`
	MaxRequestUsage      int             `json:"maxRequestUsage"`
	MaxRequestUsageTotal int             `json:"maxRequestUsageTotal"`
	BillingCycleStart    json.RawMessage `json:"billingCycleStart"`
	BillingCycleEnd      json.RawMessage `json:"billingCycleEnd"`
	DisplayMessage       string          `json:"displayMessage"`
	PlanUsage            *planUsageJSON  `json:"planUsage"`
}

func decodePeriodUsage(body []byte) (*CursorUsage, error) {
	var parsed periodUsageJSON
	if errUnmarshal := json.Unmarshal(body, &parsed); errUnmarshal != nil {
		return nil, fmt.Errorf("decode cursor response: %w", errUnmarshal)
	}
	usage := &CursorUsage{
		StartOfMonth:         parsed.StartOfMonth,
		NumRequests:          parsed.NumRequests,
		NumRequestsTotal:     parsed.NumRequestsTotal,
		MaxRequestUsage:      parsed.MaxRequestUsage,
		MaxRequestUsageTotal: parsed.MaxRequestUsageTotal,
		DisplayMessage:       strings.TrimSpace(parsed.DisplayMessage),
		CycleStart:           parseUnixMillis(parsed.BillingCycleStart),
		CycleEnd:             parseUnixMillis(parsed.BillingCycleEnd),
	}
	if parsed.PlanUsage != nil {
		usage.Plan = true
		usage.APIPercentUsed = parsed.PlanUsage.APIPercentUsed
		usage.AutoPercentUsed = parsed.PlanUsage.AutoPercentUsed
		usage.TotalPercentUsed = parsed.PlanUsage.TotalPercentUsed
		usage.LimitCents = parsed.PlanUsage.Limit
		usage.IncludedSpendCents = parsed.PlanUsage.IncludedSpend
		usage.RemainingCents = parsed.PlanUsage.Remaining
		usage.TotalSpendCents = parsed.PlanUsage.TotalSpend
	}
	if usage.StartOfMonth == "" && !usage.CycleStart.IsZero() {
		usage.StartOfMonth = usage.CycleStart.UTC().Format(time.RFC3339)
	}
	return usage, nil
}

func parseUnixMillis(raw json.RawMessage) time.Time {
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	if s == "" || s == "null" {
		return time.Time{}
	}
	n, errParse := strconv.ParseInt(s, 10, 64)
	if errParse != nil {
		if t, errRFC := time.Parse(time.RFC3339Nano, s); errRFC == nil {
			return t.UTC()
		}
		if t, errRFC := time.Parse(time.RFC3339, s); errRFC == nil {
			return t.UTC()
		}
		return time.Time{}
	}
	if n <= 0 {
		return time.Time{}
	}
	if n < 1e12 {
		return time.Unix(n, 0).UTC()
	}
	return time.UnixMilli(n).UTC()
}

func looksLikeJWT(token string) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(token)), "crsr_") {
		return false
	}
	parts := strings.Split(token, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != ""
}

func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, errDecode := base64.RawURLEncoding.DecodeString(parts[1])
	if errDecode != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if errUnmarshal := json.Unmarshal(payload, &claims); errUnmarshal != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0).UTC()
}

func tokenExpiry(token string) time.Time {
	now := time.Now()
	exp := jwtExpiry(token)
	if exp.IsZero() {
		return now.Add(tokenFallbackTTL)
	}
	until := exp.Add(-tokenRefreshSkew)
	if !until.After(now) {
		return now
	}
	return until
}

// ClearCache evicts all cached responses.
func (c *Client) ClearCache() {
	c.cacheMu.Lock()
	c.cache = make(map[string]cacheEntry)
	c.cacheMu.Unlock()
	c.tokenMu.Lock()
	c.tokens = make(map[string]tokenEntry)
	c.tokenMu.Unlock()
}

// SetCache manually seeds or overrides the cache for an apiKey.
func (c *Client) SetCache(apiKey string, usage *CursorUsage) {
	trimmedKey := strings.TrimSpace(apiKey)
	if trimmedKey == "" || usage == nil {
		return
	}
	c.cacheMu.Lock()
	c.cache[hashKey(trimmedKey)] = cacheEntry{
		usage:     usage,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.cacheMu.Unlock()
}

func hashKey(key string) string {
	hasher := sha256.New()
	hasher.Write([]byte(key))
	return hex.EncodeToString(hasher.Sum(nil))
}
