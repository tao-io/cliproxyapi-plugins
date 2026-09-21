package dashboard

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/UNICKCHENG/cliproxyapi-plugins/auth-cursor/go/internal/quota"
)

func TestRenderDashboard_Empty(t *testing.T) {
	data := DashboardData{
		Title:       "Test Dashboard",
		RefreshedAt: time.Now(),
		Accounts:    nil,
	}
	html := RenderDashboard(data)
	if !strings.Contains(html, "No Cursor Accounts Found") {
		t.Fatal("expected empty state message in HTML")
	}
	if !strings.Contains(html, "Test Dashboard") {
		t.Fatal("expected custom title in HTML")
	}
}

func TestRenderDashboard_WithAccounts(t *testing.T) {
	data := DashboardData{
		Title:       "Test Dashboard",
		RefreshedAt: time.Now(),
		Accounts: []AccountView{
			{
				ID:    "cursor-1.json",
				Name:  "Personal Cursor",
				Email: "dev@example.com",
				Usage: &quota.CursorUsage{
					StartOfMonth:         "2026-09-01T00:00:00.000Z",
					NumRequests:          45,
					NumRequestsTotal:     45,
					MaxRequestUsage:      500,
					MaxRequestUsageTotal: 500,
				},
			},
			{
				ID:    "cursor-err.json",
				Name:  "Expired Cursor <script>",
				Error: "401 Unauthorized",
			},
		},
	}
	html := RenderDashboard(data)
	if !strings.Contains(html, "dev@example.com") {
		t.Fatal("expected dev@example.com email")
	}
	if strings.Contains(html, "<script>") {
		t.Fatal("HTML should escape dangerous characters")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatal("expected escaped script tag")
	}
	if !strings.Contains(html, "401 Unauthorized") {
		t.Fatal("expected error message box")
	}
}

func TestRenderDashboard_PlanUsage(t *testing.T) {
	html := RenderDashboard(DashboardData{
		Title:       "Plan",
		RefreshedAt: time.Now(),
		Accounts: []AccountView{{
			ID:    "cursor-dread9ko.json",
			Name:  "dread9ko",
			Email: "dread9ko@gmail.com",
			Usage: &quota.CursorUsage{
				Plan:               true,
				APIPercentUsed:     100,
				AutoPercentUsed:    74,
				TotalPercentUsed:   75,
				LimitCents:         2000,
				IncludedSpendCents: 2000,
				CycleStart:         time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC),
				CycleEnd:           time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
				DisplayMessage:     "You've hit your usage limit",
			},
		}},
	})
	for _, want := range []string{
		"dread9ko@gmail.com",
		"26% remaining",
		"Third Party",
		"0% remaining",
		"You&#39;ve hit your usage limit",
		"accounts-list",
		"brand-mark",
		`id="cursor-logo"`,
		"$20/mo",
		"plan-pill-pro",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in dashboard HTML", want)
		}
	}
	for _, refuse := range []string{
		"Total 25% left",
		"Cursor-metered",
		"$20.00",
		"fast requests",
	} {
		if strings.Contains(html, refuse) {
			t.Fatalf("did not want %q in dashboard HTML", refuse)
		}
	}
	if !strings.Contains(html, "minmax(320px") {
		t.Fatal("expected the compact card grid")
	}
}

func TestRenderDashboard_FollowsColorScheme(t *testing.T) {
	html := RenderDashboard(DashboardData{Title: "Theme", RefreshedAt: time.Now()})
	for _, want := range []string{
		`name="color-scheme" content="dark"`,
		`--bg-main: #151412`,
		`class="dark"`,
		`id="cursor-logo"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in dashboard HTML", want)
		}
	}
}

func TestRenderDashboard_ThreeColumnGrid(t *testing.T) {
	html := RenderDashboard(DashboardData{
		Title:       "Cursor Quota",
		RefreshedAt: time.Date(2026, 9, 21, 10, 19, 0, 0, time.FixedZone("CEST", 2*3600)),
		Accounts: []AccountView{
			{
				Name:  "cursor-dread9ko@gmail.com.json",
				Email: "dread9ko@gmail.com",
				Usage: &quota.CursorUsage{
					Plan: true, APIPercentUsed: 100, AutoPercentUsed: 74, TotalPercentUsed: 75,
					LimitCents: 2000, IncludedSpendCents: 2000,
					CycleEnd:       time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
					DisplayMessage: "You've hit your usage limit",
				},
			},
			{
				Name:  "cursor-info@fridmanproperties.com.json",
				Email: "info@fridmanproperties.com",
				Usage: &quota.CursorUsage{
					Plan: true, APIPercentUsed: 3.2, AutoPercentUsed: 0.6, TotalPercentUsed: 0.9,
					LimitCents: 2000, IncludedSpendCents: 433,
					CycleEnd:       time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC),
					DisplayMessage: "You've used 22% of your included usage",
				},
			},
			{
				Name:  "cursor-julyanaraevskaya@gmail.com.json",
				Email: "julyanaraevskaya@gmail.com",
				Usage: &quota.CursorUsage{
					Plan: true, APIPercentUsed: 1.5, AutoPercentUsed: 59.1, TotalPercentUsed: 55,
					LimitCents: 7000, IncludedSpendCents: 7000,
					CycleEnd:       time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
					DisplayMessage: "You've hit your usage limit",
				},
			},
		},
	})
	if n := strings.Count(html, "<article"); n != 3 {
		t.Fatalf("want 3 account cards, got %d", n)
	}
	if strings.Count(html, `class="brand-mark"`) != 3 {
		t.Fatal("expected the Cursor mark on each card")
	}
	if !strings.Contains(html, "Third Party") || !strings.Contains(html, "% remaining") {
		t.Fatal("expected Cursor-dashboard pool labels")
	}
	if strings.Contains(html, "Total ") {
		t.Fatal("Total pool should not be rendered")
	}
	if strings.Contains(html, "$20.00") || strings.Contains(html, "Cursor-metered") {
		t.Fatal("usage dollar amounts should not be rendered")
	}
	if !strings.Contains(html, "$20/mo") || !strings.Contains(html, "$60/mo") {
		t.Fatal("expected subscription prices on plan pills")
	}
	if !strings.Contains(html, "plan-pill-plus") {
		t.Fatal("expected a louder Pro+ pill")
	}
	if !strings.Contains(html, "3 accounts") || !strings.Contains(html, "3 loaded") {
		t.Fatal("expected compact header counts")
	}
	if err := os.WriteFile("/tmp/cursor-quota-preview.html", []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
}
