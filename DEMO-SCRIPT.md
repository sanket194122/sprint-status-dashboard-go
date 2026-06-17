# Sprint Status Dashboard — Demo Script

## Duration: 10-15 minutes

---

## 1. OPENING (1 min)

**Say:**
> "Hi everyone, today I'm going to demo the Sprint Status Dashboard — a real-time analytics tool that gives us complete visibility into our sprint health, team workload, and delivery risks — all powered by live JIRA data and AI."

---

## 2. PROBLEM STATEMENT (2 min)

**Say:**
> "Let me start with WHY we built this. As Scrum Masters and delivery leads, we face these challenges every sprint:"

**Pain Points (show on screen or verbally):**

| # | Problem | Impact |
|---|---|---|
| 1 | **No single view of sprint health** | We check JIRA boards, filters, and spreadsheets separately |
| 2 | **Reactive, not proactive** | We discover risks in standup or at sprint end — too late |
| 3 | **No workload visibility** | Can't see who's overloaded vs who has bandwidth |
| 4 | **Manual status reporting** | Spend 30+ min creating sprint reports for stakeholders |
| 5 | **No epic delivery risk tracking** | Epics silently slip without anyone noticing until review |
| 6 | **No historical comparison** | Can't answer "Are we improving sprint over sprint?" |

**Say:**
> "This dashboard solves ALL of these in one place, with zero manual effort."

---

## 3. SOLUTION OVERVIEW (1 min)

**Say:**
> "The dashboard is a single-page web app that:
> - Fetches live data from JIRA every 10 minutes
> - Analyzes sprint metrics automatically
> - Uses AI (GPT-4o) for intelligent risk assessment
> - Sends Teams notifications when things need attention
> - Works for ANY team, ANY project, ANY sprint — just select from dropdowns"

---

## 4. LIVE DEMO WALKTHROUGH (8-10 min)

### 4a. Selector Panel (30 sec)
**Action:** Show the top selector bar

**Say:**
> "First — the selector. Enter your project key, click Load, and it fetches all teams and sprints from JIRA. Select your team, sprint, and click Go. Works for Titans, Sapphire, or any team in CXDV."

---

### 4b. Hero Section (30 sec)
**Action:** Point to the big percentage number and health badge

**Say:**
> "The hero shows our sprint completion percentage — animated in real-time — and the overall health status: Healthy, At Risk, or Unhealthy. This is the first thing you see — one glance tells you where you stand."

---

### 4c. Sprint Timeline (30 sec)
**Action:** Point to the progress bar

**Say:**
> "The timeline is weekend-aware. It only counts working days, so if 6 of 10 working days have passed, it shows 60% — not inflated by weekends. You see days passed, days remaining, and the working day count."

---

### 4d. Stat Cards (30 sec)
**Action:** Point to the 4 cards (Total, Completed, In Progress, Remaining)

**Say:**
> "Four key numbers. These animate up when data loads. Hover on any card and see the gradient effect. Simple but tells you everything at a glance."

---

### 4e. Charts (30 sec)
**Action:** Point to donut and bar chart

**Say:**
> "The donut shows Done vs In Progress vs To Do proportionally. The bar chart breaks it down per person — who's completed what, who has work remaining. Both are animated."

---

### 4f. Time vs Work Comparison (30 sec)
**Action:** Point to the two progress bars

**Say:**
> "This is critical — it compares time elapsed vs work completed. If time is at 60% but work is at 40%, you're behind. The badge shows '+X% ahead' in green or '-X% behind' in red. Instant velocity check."

---

### 4g. Epic Delivery Risk (1 min)
**Action:** Point to the epic table, click a JIRA link

**Say:**
> "This is where AI shines. Each epic gets a risk assessment — On Track, At Risk, or Not Deliverable. The algorithm uses weekend-adjusted timelines and velocity ratios. When AI is enabled, it gives natural language recommendations like 'Needs 1.5x acceleration' or 'Consider scope reduction.'
> 
> Notice — epic names are clickable JIRA links. The Owner column shows the actual epic assignee from JIRA, not a guess. And the 🔔 button lets you notify the owner directly in Teams."

---

### 4h. Team Contribution (30 sec)
**Action:** Click on a team member name to expand

**Say:**
> "Click any name to drill down. You see their completed issues and remaining issues — with JIRA links, story points, and current status. No more asking 'what's left on your plate?'"

---

### 4i. Team Capacity Heatmap (1 min)
**Action:** Hover over a bar to show tooltip

**Say:**
> "This is the workload heatmap. Each row is a team member. The bar shows Done (green), In Progress (yellow), and To Do (gray) proportionally. 
>
> The badge tells you: Overloaded, High, Balanced, or Low. Hover on any bar for detailed breakdown — points, percentages, load factor, stuck items.
>
> The AI insight at the top tells you immediately if there's a workload imbalance and who can help."

---

### 4j. Advanced Metrics (1 min)
**Action:** Point to forecast, focus factor, availability gap

**Say:**
> "Three key predictive metrics:
> - **Sprint Forecast** — 'Xd early' or 'Xd late' based on current daily velocity
> - **Focus Factor** — percentage of work that's planned (Stories/Tasks) vs unplanned (Bugs). Higher is better.
> - **Availability Gap** — compares remaining capacity vs remaining work. Tells you if you're over or under-committed."

---

### 4k. Daily Throughput (20 sec)
**Action:** Hover over bars

**Say:**
> "Points completed per working day. Shows acceleration or deceleration patterns. If bars are getting shorter as sprint ends — that's a problem."

---

### 4l. Alert Panels (1 min)
**Action:** Point to Aging WIP, Carry-Over Risk, Scope Creep, Unassigned

**Say:**
> "Four alert panels that surface problems:
> - **Aging WIP** — items stuck 3+ days, may be blocked
> - **Carry-Over Risk** — items likely to spill into next sprint
> - **Scope Creep** — items added AFTER sprint started (shows percentage)
> - **Unassigned Work** — tickets no one's picked up
>
> All ticket IDs are clickable JIRA links."

---

### 4m. Sprint Score Card (20 sec)
**Action:** Point to the grade

**Say:**
> "A single grade — A, B, C, or D — based on commitment vs delivery, scope discipline, team focus, and execution quality. Final grade applies at sprint end."

---

### 4n. Teams Integration (1 min)
**Action:** Click "🔔 Alert Team" button, show the modal, then click Send

**Say:**
> "Two integration buttons:
> - **Alert Team** — evaluates all risk rules and sends Teams notifications with @mentions to the relevant people. Epics at risk? Tags the epic owner. Items stuck? Tags the assignee. Team-level alerts tag the Scrum Master.
> - **Share Report in Teams** — sends a beautiful formatted sprint status report to our Teams group with all key metrics, epic status, and top performers.
>
> Each notification includes an AI-generated witty message to keep it fun."

---

### 4o. Dark Mode + Export (20 sec)
**Action:** Toggle dark/light mode, click Export PDF

**Say:**
> "Dark mode toggle for preference. Export PDF generates a print-ready version for stakeholders who prefer documents."

---

## 5. TECHNICAL HIGHLIGHTS (1 min)

**Say:**
> "Under the hood:
> - **Node.js server** — zero dependencies, just native HTTP
> - **JIRA REST API v3** — real-time data, filtered by project + team + sprint
> - **GitHub Models (GPT-4o)** — AI risk analysis, witty notifications, sprint retrospective
> - **Teams Adaptive Cards** — rich formatted notifications with @mentions
> - **Weekend-aware calculations** — excludes Sat/Sun from all metrics
> - **One-click setup** — double-click a .bat file and it runs
> - **Works for any team** — just select from dropdown, no code changes"

---

## 6. CLOSING (30 sec)

**Say:**
> "To summarize — this dashboard gives us:
> 1. **Real-time visibility** — no more checking 5 different places
> 2. **Proactive risk detection** — know before it's too late
> 3. **AI-powered insights** — not just data, but recommendations
> 4. **One-click reporting** — no more manual sprint reports
> 5. **Team accountability** — @mention the right person at the right time
>
> Any questions?"

---

## BACKUP: Anticipated Questions

| Question | Answer |
|---|---|
| "How often does data refresh?" | Every 10 minutes automatically, or click refresh manually |
| "Does it work for other teams?" | Yes — just select any team from the dropdown |
| "What if AI is unavailable?" | Falls back to formula-based analysis, dashboard works fully without AI |
| "Can others use it?" | Yes — just need Node.js + JIRA API token. Share 3 files. |
| "Where's the data stored?" | Sprint history saved locally as JSON. No external database needed. |
| "Can we customize alert rules?" | Yes — thresholds are configurable in server.js (e.g., stuck days, scope creep %) |
