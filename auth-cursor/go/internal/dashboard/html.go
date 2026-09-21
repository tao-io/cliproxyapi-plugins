package dashboard

import (
	"fmt"
	"html"
	"math"
	"strings"
	"time"

	"github.com/UNICKCHENG/cliproxyapi-plugins/auth-cursor/go/internal/quota"
)

// AccountView contains UI display data for a single Cursor credential.
type AccountView struct {
	ID            string             `json:"id"`
	AuthIndex     string             `json:"auth_index"`
	Name          string             `json:"name"`
	Email         string             `json:"email"`
	StatusMessage string             `json:"status_message"`
	Usage         *quota.CursorUsage `json:"usage,omitempty"`
	Error         string             `json:"error,omitempty"`
	Cached        bool               `json:"cached"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

// DashboardData holds the view model passed to RenderDashboard.
type DashboardData struct {
	Title          string
	Accounts       []AccountView
	RefreshedAt    time.Time
	RefreshURL     string
	APIEndpointURL string
}

// cursorLogoSymbol is the official Cursor app mark (viewBox 512).
const cursorLogoSymbol = `<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" aria-hidden="true" focusable="false" style="position:absolute;overflow:hidden"><symbol id="cursor-logo" viewBox="0 0 512 512" fill="none"><clipPath id="cursor-logo-clip"><path d="M96 73h320.735v365.65H96z"/></clipPath><path d="M512.001 325.499c0 7.124 0 14.24-.041 21.364-.035 5.999-.103 11.998-.268 17.99-.356 13.068-1.124 26.245-3.448 39.169-2.359 13.109-6.205 25.306-12.266 37.222-5.958 11.703-13.746 22.419-23.029 31.709-9.29 9.291-20 17.072-31.71 23.03-11.909 6.061-24.113 9.907-37.222 12.266-12.923 2.324-26.101 3.092-39.169 3.448-5.999.165-11.991.233-17.99.268-7.123.048-14.24.041-21.364.041h-139c-7.124 0-14.24 0-21.364-.041-5.999-.035-11.998-.103-17.99-.268-13.068-.356-26.245-1.124-39.169-3.448-13.109-2.359-25.306-6.205-37.2219-12.266-11.7034-5.958-22.4195-13.746-31.7095-23.03-9.29-9.29-17.0717-19.999-23.0296-31.709-6.06082-11.909-9.90709-24.113-12.26559-37.222-2.32422-12.924-3.092104-26.101-3.448621-39.169-.164547-5.999-.2331075-11.991-.267388-17.99-.02742444-7.124-.02742444-14.24-.02742444-21.364v-139c0-7.124 0-14.24.04113664-21.364.0342805-5.999.1028418-11.998.2673878-17.99.356517-13.068 1.124399-26.245 3.448619-39.169 2.3585-13.1091 6.20477-25.3061 12.26558-37.222 5.9579-11.7034 13.7465-22.4195 23.0296-31.7095 9.2901-9.29 19.9993-17.0717 31.7095-23.0296 11.9091-6.06082 24.1129-9.90709 37.2222-12.26559 12.923-2.32422 26.101-3.092101 39.168-3.448618 6-.164547 11.992-.2331075 17.991-.2673881 7.117-.03428046 14.233-.03428046 21.357-.03428046h139c7.124 0 14.24 0 21.364.04113656 5.999.0342806 11.998.102842 17.99.267388 13.068.356517 26.245 1.124402 39.169 3.448622 13.109 2.3585 25.306 6.20477 37.222 12.26553 11.703 5.958 22.419 13.7465 31.709 23.0297 9.29 9.29 17.072 19.9992 23.03 31.7094 6.061 11.9091 9.907 24.113 12.266 37.2222 2.324 12.923 3.092 26.101 3.448 39.169.165 5.999.233 11.991.268 17.99.048 7.123.041 14.24.041 21.364v139z" fill="#14120b"/><path d="M186.501 3.99902h139c7.125 0 14.231-.00004 21.341.04102 5.985.0342 11.952.10221 17.903.26562h.001c13.003.35475 25.943 1.11709 38.569 3.3877 12.775 2.29828 24.592 6.03144 36.118 11.89354v-.001c10.973 5.5867 21.052 12.8384 29.848 21.4571l.847.8379c8.993 8.993 16.525 19.3593 22.292 30.6933l3.565-1.8135-3.565 1.8145c5.678 11.1575 9.36 22.6024 11.674 34.9208l.219 1.195c2.129 11.837 2.932 23.95 3.316 36.132l.072 2.438c.164 5.958.232 11.919.266 17.904v.004c.048 7.107.041 14.207.041 21.337v129.344l-.007-.007v9.656c0 7.125 0 14.231-.041 21.341-.034 5.985-.102 11.952-.266 17.903v.001c-.354 13.003-1.117 25.944-3.387 38.569-2.227 12.376-5.8 23.853-11.351 35.037l-.543 1.08c-5.767 11.327-13.306 21.701-22.293 30.695-8.993 8.993-19.361 16.526-30.695 22.293-11.518 5.861-23.342 9.595-36.116 11.894-11.837 2.128-23.95 2.931-36.132 3.315l-2.438.072c-5.958.164-11.919.232-17.904.266h-.004c-7.107.048-14.207.041-21.337.041h-139c-7.125 0-14.231 0-21.341-.041-5.985-.034-11.952-.102-17.903-.266h-.001c-13.003-.355-25.944-1.117-38.57-3.387-12.7742-2.299-24.5914-6.032-36.1165-11.894h.001c-10.974-5.587-21.0534-12.837-29.8496-21.456l-.8467-.838c-8.7118-8.712-16.0531-18.713-21.7461-29.634l-.5459-1.06-.544-1.081c-5.3709-10.816-8.8904-21.919-11.12983-33.84l-.21973-1.196c-2.27061-12.625-3.03294-25.566-3.38769-38.569v-.001c-.16342-5.958-.23143-11.918-.26563-17.903-.02736-7.112-.02734-14.219-.02734-21.341v-139c0-7.125-.00005-14.231.04101-21.341.0342-5.985.10222-11.952.26563-17.903v-.001c.35475-13.003 1.11699-25.944 3.38769-38.57 2.29829-12.7743 6.03159-24.5916 11.89359-36.1166l-.001-.001c5.7665-11.3269 13.3073-21.6999 22.2939-30.6934 8.9933-8.9932 19.36-16.5261 30.6944-22.2929h.0009c11.5174-5.8615 23.3407-9.59617 36.1149-11.89455 11.837-2.12878 23.951-2.93017 36.133-3.31446l2.438-.07226c5.958-.16342 11.919-.23142 17.904-.26563l-.001-.00097c7.104-.03422 14.211-.03321 21.335-.03321z" stroke="#edecec" stroke-opacity=".2" stroke-width="8" fill="#14120b"/><g clip-path="url(#cursor-logo-clip)"><path d="M410.344 159.545 263.964 75.0339c-4.7-2.7145-10.5-2.7145-15.2 0L102.391 159.545c-3.9515 2.282-6.391 6.501-6.391 11.071v170.418c0 4.569 2.4395 8.789 6.391 11.07l146.379 84.512c4.701 2.714 10.501 2.714 15.201 0l146.38-84.512c3.951-2.281 6.391-6.501 6.391-11.07v-170.418c0-4.57-2.44-8.789-6.391-11.071zm-9.195 17.902-141.308 244.751c-.955 1.65-3.477.976-3.477-.934v-160.261c0-3.203-1.711-6.164-4.487-7.772l-138.786-80.127c-1.65-.956-.976-3.478.934-3.478h282.616c4.013 0 6.522 4.35 4.515 7.828h-.007z" fill="#edecec"/></g></symbol></svg>`

const cursorMarkSVG = `<svg viewBox="0 0 512 512" width="18" height="18" aria-hidden="true"><use href="#cursor-logo"/></svg>`

// RenderDashboard generates Cursor-dashboard-shaped quota cards.
func RenderDashboard(data DashboardData) string {
	var b strings.Builder

	title := data.Title
	if title == "" {
		title = "Cursor Quota"
	}
	refreshURL := data.RefreshURL
	if refreshURL == "" {
		refreshURL = "?refresh=1"
	}
	apiEndpointURL := data.APIEndpointURL
	if apiEndpointURL == "" {
		apiEndpointURL = "/v0/management/cursor/usage"
	}

	var activeCount, errorCount int
	for _, acc := range data.Accounts {
		if acc.Error != "" {
			errorCount++
			continue
		}
		activeCount++
	}

	b.WriteString(`<!DOCTYPE html>
<html lang="en" class="dark" style="background:#151412;color:#ececec">
<head>
  <meta charset="utf-8">
  <meta name="color-scheme" content="dark">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>` + html.EscapeString(title) + `</title>
  <style>
    html { background: #151412; color: #ececec; color-scheme: dark; }
    :root {
      color-scheme: dark;
      --bg-main: #151412;
      --bg-card: #1c1b19;
      --border: #2c2a26;
      --text: #ececec;
      --muted: #8a8680;
      --teal: #3ee0b5;
      --danger: #f87171;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: var(--bg-main);
      color: var(--text);
      font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      font-size: 15px;
      line-height: 1.45;
      padding: 1.35rem 1.15rem 1.75rem;
      min-height: 100vh;
    }
    .container { max-width: 1180px; margin: 0 auto; }
    header {
      display: flex;
      flex-wrap: wrap;
      justify-content: space-between;
      align-items: flex-start;
      gap: 0.75rem 1rem;
      margin-bottom: 1.15rem;
    }
    .title-group h1 {
      font-size: 1.7rem;
      font-weight: 700;
      letter-spacing: -0.03em;
    }
    .status {
      display: flex;
      align-items: center;
      gap: 0.55rem;
      margin-top: 0.45rem;
      font-size: 0.92rem;
      color: var(--muted);
    }
    .status-tick {
      width: 3px; height: 0.95rem;
      background: var(--teal);
      border-radius: 2px;
      flex-shrink: 0;
    }
    .status .ok { color: var(--teal); font-weight: 650; }
    .brand-mark {
      width: 22px; height: 22px;
      display: grid; place-items: center;
      flex-shrink: 0;
    }
    .brand-mark svg { display: block; width: 100%; height: 100%; }
    .actions { display: flex; gap: 0.45rem; }
    .btn {
      display: inline-flex; align-items: center; gap: 0.35rem;
      background: transparent; color: var(--text);
      font-weight: 550; font-size: 0.85rem;
      padding: 0.4rem 0.9rem; border-radius: 9999px;
      text-decoration: none; border: 1px solid var(--border);
    }
    .btn:hover { background: #252320; }
    .btn:focus-visible { outline: 2px solid var(--teal); outline-offset: 2px; }
    .accounts-list {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
      gap: 0.85rem;
      align-items: stretch;
    }
    .account-card {
      background: var(--bg-card);
      border: 1px solid var(--border);
      border-radius: 0.85rem;
      padding: 1.05rem 1.05rem 0.95rem;
      min-width: 0;
      display: flex;
      flex-direction: column;
    }
    .card-top {
      display: flex;
      align-items: center;
      gap: 0.55rem;
      min-width: 0;
      margin-bottom: 0.7rem;
    }
    .card-email {
      font-size: 1.02rem; font-weight: 650; color: #fff;
      overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    }
    .plan-row {
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      gap: 0.4rem 0.5rem;
      margin: 0 0 0.95rem;
    }
    .plan-kicker {
      font-size: 0.88rem;
      color: var(--muted);
      font-weight: 500;
    }
    .plan-pill {
      display: inline-flex;
      align-items: baseline;
      gap: 0.35rem;
      border-radius: 9999px;
      font-weight: 650;
      letter-spacing: -0.02em;
      line-height: 1;
    }
    .plan-pill .plan-price { font-weight: 600; opacity: 0.72; }
    .plan-pill-quiet {
      font-size: 0.8rem;
      padding: 0.2rem 0.55rem;
      color: var(--muted);
    }
    .plan-pill-pro {
      font-size: 0.82rem;
      padding: 0.22rem 0.62rem 0.24rem;
      background: #d8d8dc;
      color: #18181b;
      box-shadow: 0 1px 0 rgba(255,255,255,.18), 0 6px 14px rgba(0,0,0,.22);
    }
    .plan-pill-plus {
      font-size: 0.95rem;
      padding: 0.28rem 0.78rem 0.3rem;
      background: #f4f4f5;
      color: #111;
      font-weight: 750;
      box-shadow: 0 0 0 1px rgba(255,255,255,.1), 0 8px 18px rgba(0,0,0,.32);
    }
    .pool { margin: 0 0 0.85rem; }
    .pool-head {
      display: flex;
      align-items: baseline;
      gap: 0.4rem 0.65rem;
      margin-bottom: 0.32rem;
      font-size: 0.9rem;
    }
    .pool-name { font-weight: 650; color: var(--text); }
    .pool-remaining { color: var(--teal); font-weight: 650; margin-left: auto; }
    .pool-remaining.is-empty { color: var(--text); }
    .pool-reset { color: var(--muted); font-size: 0.82rem; white-space: nowrap; }
    .progress-track {
      height: 6px;
      background: #2a2824;
      border-radius: 9999px;
      overflow: hidden;
    }
    .progress-fill {
      height: 100%;
      background: var(--teal);
      border-radius: 9999px;
    }
    .pool-sub { margin-top: auto; padding-top: 0.35rem; font-size: 0.82rem; color: var(--muted); }
    .error-box {
      background: rgba(248, 113, 113, 0.1);
      border: 1px solid rgba(248, 113, 113, 0.28);
      color: var(--danger);
      padding: 0.65rem 0.75rem;
      border-radius: 0.5rem;
      font-size: 0.8rem;
      margin-top: 0.2rem;
    }
    .empty-state {
      text-align: center;
      padding: 2.6rem 1.3rem;
      background: var(--bg-card);
      border: 1px dashed var(--border);
      border-radius: 0.85rem;
    }
    .empty-state h3 { font-size: 1.05rem; margin-bottom: 0.4rem; }
    .empty-state p { color: var(--muted); font-size: 0.8rem; margin-bottom: 1rem; }
    @media (max-width: 720px) {
      .accounts-list { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  ` + cursorLogoSymbol + `
  <div class="container">
    <header>
      <div class="title-group">
        <h1>` + html.EscapeString(title) + `</h1>
        <p class="status"><span class="status-tick"></span>` +
		fmt.Sprintf("%d accounts", len(data.Accounts)) +
		` · <span class="ok">` + fmt.Sprintf("%d loaded", activeCount) + `</span>` +
		errorSuffix(errorCount) +
		`</p>
      </div>
      <div class="actions">
        <a href="` + html.EscapeString(refreshURL) + `" class="btn">Refresh quota</a>
        <a href="` + html.EscapeString(apiEndpointURL) + `" target="_blank" class="btn">API JSON</a>
      </div>
    </header>
    <div class="accounts-list">`)

	if len(data.Accounts) == 0 {
		b.WriteString(`
      <div class="empty-state" style="grid-column: 1 / -1">
        <h3>No Cursor Accounts Found</h3>
        <p>Signing in at cursor.com does not add the account to the proxy. Create a Dashboard API key for that user, then import it with <code>--cursor-login</code> (one key per account).</p>
        <a href="` + html.EscapeString(refreshURL) + `" class="btn">Check Again</a>
      </div>`)
	} else {
		for _, acc := range data.Accounts {
			renderAccountCard(&b, acc)
		}
	}

	b.WriteString(`
    </div>
  </div>
</body>
</html>`)

	return b.String()
}

func renderAccountCard(b *strings.Builder, acc AccountView) {
	name := acc.Name
	if name == "" {
		name = acc.ID
	}
	email := acc.Email
	if email == "" {
		email = name
	}

	badge := planBadge(acc)

	b.WriteString(`
      <article class="account-card">
        <div class="card-top">
          <span class="brand-mark">` + cursorMarkSVG + `</span>
          <div class="card-email" title="` + html.EscapeString(email) + `">` + html.EscapeString(email) + `</div>
        </div>
        <div class="plan-row">
          <span class="plan-kicker">Plan</span>
          <span class="plan-pill ` + badge.class + `">` + html.EscapeString(badge.name))
	if badge.price != "" {
		b.WriteString(`<span class="plan-price">` + html.EscapeString(badge.price) + `</span>`)
	}
	b.WriteString(`</span>
        </div>`)

	if acc.Error != "" {
		b.WriteString(`
        <div class="error-box">Failed to fetch quota: ` + html.EscapeString(acc.Error) + `</div>`)
	} else if acc.Usage != nil {
		u := acc.Usage
		reset := ""
		if in := u.FormatResetIn(time.Now()); in != "" {
			reset = "Resets in " + in
		}
		if u.Plan {
			writePool(b, "Cursor", u.AutoPercentUsed, reset)
			writePool(b, "Third Party", u.APIPercentUsed, reset)
			if u.DisplayMessage != "" {
				b.WriteString(`
        <p class="pool-sub">` + html.EscapeString(u.DisplayMessage) + `</p>`)
			}
		} else {
			writePool(b, "Fast requests", u.UsagePercentage(), reset)
			b.WriteString(`
        <p class="pool-sub">` + html.EscapeString(u.FormatStatusMessage()) + `</p>`)
		}
	}

	b.WriteString(`
      </article>`)
}

func writePool(b *strings.Builder, name string, used float64, reset string) {
	left := remainingPercent(used)
	remainClass := "pool-remaining"
	if left <= 0 {
		remainClass += " is-empty"
	}
	b.WriteString(`
        <div class="pool">
          <div class="pool-head">
            <span class="pool-name">` + html.EscapeString(name) + `</span>
            <span class="` + remainClass + `">` + fmt.Sprintf("%.0f%% remaining", left) + `</span>`)
	if reset != "" {
		b.WriteString(`
            <span class="pool-reset">` + html.EscapeString(reset) + `</span>`)
	}
	b.WriteString(`
          </div>
          <div class="progress-track">
            <div class="progress-fill" style="width:` + fmt.Sprintf("%.1f%%", left) + `"></div>
          </div>
        </div>`)
}

type planBadgeView struct {
	name  string
	price string
	class string
}

func planBadge(acc AccountView) planBadgeView {
	if acc.Error != "" {
		return planBadgeView{name: "Error", class: "plan-pill-quiet"}
	}
	if acc.Usage == nil {
		return planBadgeView{name: "Pending", class: "plan-pill-quiet"}
	}
	if !acc.Usage.Plan {
		return planBadgeView{name: "Fast", class: "plan-pill-quiet"}
	}
	if acc.Usage.LimitCents >= 6000 {
		return planBadgeView{name: "Pro+", price: "$60/mo", class: "plan-pill-plus"}
	}
	return planBadgeView{name: "Pro", price: "$20/mo", class: "plan-pill-pro"}
}

func remainingPercent(used float64) float64 {
	return math.Min(100.0, math.Max(0.0, 100-used))
}

func errorSuffix(errorCount int) string {
	if errorCount <= 0 {
		return ""
	}
	return fmt.Sprintf(" · %d error", errorCount)
}
