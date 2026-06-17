````skill
---
name: sprint-status-dashboard
description: Creates a complete real-time JIRA sprint analytics dashboard with AI-powered risk analysis, Teams notifications, and Framer/Raycast dark UI. Generates main.go + dashboard.html — zero external Go dependencies, compiles to a single binary.
---

# Sprint Status Dashboard Skill (Go)

## Overview

This skill generates a complete, production-ready sprint analytics dashboard. The output is:
- `main.go` — Go HTTP server (zero external dependencies, ~1000 lines)
- `go.mod` — Module file (Go 1.21+)
- `dashboard.html` — Single-page frontend (inline CSS + JS, Chart.js via CDN, ~1300 lines)
- `Start-Dashboard.bat` — One-click Windows launcher (auto-builds + opens browser)

## Tools

- **Write** (generate main.go, go.mod, dashboard.html, Start-Dashboard.bat)
- **Bash** (verify with `go build .`, create directories)
- **Read** (reference existing files if upgrading)

## Inputs

| Input | Required | Description |
|---|---|---|
| Project Key | Yes | JIRA project key (e.g., CXDV) |
| Team Name | Yes | Team name as in JIRA dropdown (e.g., Titans) |
| Sprint Name | Yes | Sprint name fragment to match (e.g., CX.26.3.191) |
| JIRA Base URL | No | Defaults to https://nice-ce-cxone-prod.atlassian.net |

## Phase 1: Setup

1. Create directory structure:
   ```
   sprint-dashboard/
     main.go
     go.mod
     dashboard.html
     data/
     Start-Dashboard.bat
   ```

2. Confirm user has:
   - Go 1.21+ installed (https://go.dev/dl/)
   - JIRA API token
   - Environment variables: CONFLUENCE_USERNAME, CONFLUENCE_TOKEN

## Phase 2: Generate main.go

### Go Standard Library Only
```
net/http, encoding/json, crypto/tls, encoding/base64,
sync, time, os, path/filepath, io, fmt, log, sort,
strings, math, bytes
```

### HTTP Routing (switch-based, NOT ServeMux)
```go
handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    switch r.URL.Path {
    case "/api/defaults": handleDefaults(w, r)
    case "/api/all": handleAll(w, r)
    case "/api/sprints": handleSprints(w, r)
    case "/api/teams": handleTeams(w, r)
    case "/api/history": handleHistory(w, r)
    case "/api/refresh": handleRefresh(w, r)
    case "/api/alert": handleAlert(w, r)
    case "/api/alert-all": handleAlertAll(w, r)
    case "/api/share-report": handleShareReport(w, r)
    default: handleRoot(w, r)
    }
})
```

### JIRA Functions
- `jiraGet(path)` — GET with keep-alive, in-memory cache
- `jiraPost(path, payload)` — POST for v3 search
- `findBoard(projectKey)` — discover board ID
- `findSprint(boardID, name)` — search active/future/closed
- `getSprintIssues(sprintID, project, team)` — JQL + cursor pagination
- `parseIssue(raw)` — extract fields including assignee email
- `handleTeams(w, r)` — discover team field, extract unique values

### Analytics Functions
- `computeTimeProgress(start, end)` — weekend-aware with `countWorkingDays`
- `computeSummary(issues, tp)` — totals, perPerson, perEpic
- `computePerPerson(issues)` — assigned/completed/remaining
- `computePerEpic(issues, tp)` — with top contributor as owner
- `calculateEpicRisk(total, completed, remaining, timeElapsed)` — velocity ratio
- `assessSprintHealth(summary, tp)` — composite score
- `computeAdvanced(issues, tp, start, end)` — forecast, aging, scope creep, carry-over, focus, unassigned, reopened, scorecard, heatmap
- `computeHeatmap(issues, agingWIP, totalPts)` — load factor per person

### Critical Go Patterns
- All slices MUST be initialized to empty (`[]Type{}`) — never nil (nil → JSON null breaks frontend)
- Date parsing: try RFC3339 first, fallback to "2006-01-02"
- Empty sprint result: return `{"empty": true, "message": "..."}` not HTTP 500
- Cache with `sync.RWMutex` + 5-minute TTL
- HTTP client with keep-alive for connection reuse
- `writeJSON` helper with CORS headers

### Teams Notification Functions
- `sendTeamsNotification(title, facts)` — Adaptive Card v1.5 via webhook
- `handleAlert(w, r)` — single notification from frontend
- `handleAlertAll(w, r)` — evaluate rules + send all
- `handleShareReport(w, r)` — rich sprint report card

### Sprint History
- `saveHistory(sprintName, summary, advanced, project, team)` — append to JSON
- `getHistory(project, team)` — read from JSON
- Deduplicated by sprint name + date

## Phase 3: Generate dashboard.html

### UI Design System (Framer/Raycast Dark)
- **Background**: #09090b with animated mesh gradient (4 radial orbs, 20s meshMove animation)
- **Cards**: rgba(255,255,255,0.03) with 1px border, backdrop-filter blur(8px)
- **Accent**: #a78bfa (purple), #60a5fa (blue), #f472b6 (pink)
- **Success/Warning/Danger**: #4ade80 / #fbbf24 / #f87171
- **Font**: Inter (400-900 weights via Google Fonts CDN)
- **Animations**: fadeUp with scale(0.98→1), spring easing cubic-bezier(0.16,1,0.3,1)
- **Chart colors**: #4ade80 (done), #fbbf24 (wip), #f87171 (remaining)
- **Heatmap**: Glass-effect rows with floating tooltip (backdrop-filter blur 16px)

### Light Mode Variables (alternate theme)
- Background: #fafafa, Cards: rgba(255,255,255,0.9), Accent: #7c3aed

### Sections (in order)
1. Sticky topbar with gradient title + theme toggle
2. Selector panel (project, Load btn, team dropdown, sprint dropdown, Go btn, Alert Team btn, Share Report btn)
3. Empty state (🔍 + message + Switch to Active Sprint button)
4. Loading with funny rotating messages
5. Hero card (team name, sprint, animated percentage, health badge)
6. Timeline bar (gradient + shimmer animation)
7. 4 stat cards (animated counting numbers)
8. Charts row (donut + stacked bar)
9. Time vs Work comparison
10. AI insight banner
11. Epic Delivery Risk table (JIRA links, alert buttons)
12. Team Contribution (clickable drill-down with JIRA links)
13. Team Capacity Heatmap (glass tooltip, insight banner)
14. Advanced metrics (Forecast, Focus Factor, Availability Gap)
15. Daily Throughput bars (staggered animation)
16. Alert panels: Aging WIP, Carry-Over, Scope Creep, Unassigned
17. Re-opened Issues
18. Sprint Score Card
19. Sprint Trends
20. AI Retro Preview
21. Footer (refresh btn, export PDF)
22. Confirmation modal (Alert Team)

### Key JavaScript Functions
- `jiraLink(key)` — clickable link to JIRA issue
- `animateCount(id, target)` — counting animation
- `loadDefaults()`, `loadProjectOptions()`, `applySelection()`
- `fetchAll()` with loading messages + empty state handling
- `showEmptyState(msg)` + `switchToActiveSprint()`
- `render(sprint, summary, health, issues, ai, advanced, history)`
- `renderHeatmap(heatmap)` — glass rows + floating tooltip
- `renderTrends(history)` — velocity bars + comparison table
- `alertOne()`, `alertAll()`, `confirmAlertAll()`, `shareReport()`
- `manualRefresh()`, `exportPDF()`, `toggleTheme()`

## Phase 4: Generate Start-Dashboard.bat

```bat
@echo off
title Sprint Status Dashboard
cd /d "%~dp0"
if not exist "sprint-dashboard.exe" (
    echo Building...
    go build -o sprint-dashboard.exe .
    if errorlevel 1 (echo BUILD FAILED. Install Go: https://go.dev/dl/ & pause & exit /b 1)
)
timeout /t 1 /nobreak >nul
start http://localhost:8501
sprint-dashboard.exe
pause
```

## Phase 5: Verification

1. Run `go build .` — must compile with zero errors
2. Set env vars and run `./sprint-dashboard.exe`
3. Open http://localhost:8501 — dashboard loads with live JIRA data
4. Select wrong sprint → beautiful empty state appears
5. Hover heatmap bars → glass tooltip appears
6. Click Alert Team → modal → send → Teams notification received

## Output

```
sprint-dashboard/
├── main.go              (Go server, ~1000 lines)
├── go.mod               (Go 1.21, zero deps)
├── dashboard.html       (Frontend, ~1300 lines)
├── Start-Dashboard.bat  (One-click launcher)
└── data/                (Auto-created for sprint history)
```

Double-click `Start-Dashboard.bat` → first run compiles → opens browser → dashboard loads.
````
