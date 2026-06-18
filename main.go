package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- Config ----------

var (
	jiraBaseURL       = envOr("JIRA_BASE_URL", "https://nice-ce-cxone-prod.atlassian.net")
	username          = envOr("CONFLUENCE_USERNAME", os.Getenv("JIRA_USER"))
	token             = envOr("CONFLUENCE_TOKEN", os.Getenv("JIRA_API_TOKEN"))
	sprintName        = envOr("SPRINT_NAME", "CX.26.3.191")
	teamName          = envOr("TEAM_NAME", "Titans")
	projectKey        = envOr("PROJECT_KEY", "CXDV")
	port              = envOr("DASHBOARD_PORT", "8501")
	awsRegion         = envOr("AWS_REGION", "us-east-1")
	bedrockModel      = envOr("BEDROCK_MODEL", "us.anthropic.claude-sonnet-4-20250514-v1:0")
	awsProfile        = envOr("AWS_PROFILE", "default")
	teamsWebhookURL   = os.Getenv("TEAMS_WEBHOOK_URL")
	teamsGroupWebhook = os.Getenv("TEAMS_GROUP_WEBHOOK_URL")
	authHeader        string
	dashboardHTML     string
	httpClient        *http.Client
)

// ---------- Cache ----------

type cacheEntry struct {
	data []byte
	ts   time.Time
}

var (
	cache    = map[string]cacheEntry{}
	cacheMu  sync.RWMutex
	cacheTTL = 5 * time.Minute
)

func getCached(key string) ([]byte, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	e, ok := cache[key]
	if !ok || time.Since(e.ts) > cacheTTL {
		return nil, false
	}
	return e.data, true
}

func setCache(key string, data []byte) {
	cacheMu.Lock()
	cache[key] = cacheEntry{data: data, ts: time.Now()}
	cacheMu.Unlock()
}

func clearCache() {
	cacheMu.Lock()
	cache = map[string]cacheEntry{}
	cacheMu.Unlock()
}

// ---------- Models ----------

type Issue struct {
	Key              string  `json:"key"`
	Summary          string  `json:"summary"`
	Status           string  `json:"status"`
	StatusCategory   string  `json:"statusCategory"`
	Assignee         string  `json:"assignee"`
	AssigneeEmail    string  `json:"assigneeEmail"`
	StoryPoints      float64 `json:"storyPoints"`
	EpicKey          string  `json:"epicKey"`
	EpicName         string  `json:"epicName"`
	IssueType        string  `json:"issueType"`
	Created          string  `json:"created"`
	Updated          string  `json:"updated"`
	ResolutionDate   string  `json:"resolutionDate"`
	StatusChangeDate string  `json:"statusChangeDate"`
}

type Sprint struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

type TimeProgress struct {
	TotalDays          int     `json:"totalDays"`
	DaysPassed         int     `json:"daysPassed"`
	DaysRemaining      int     `json:"daysRemaining"`
	TimeElapsedPct     float64 `json:"timeElapsedPct"`
	WorkingDaysTotal   int     `json:"workingDaysTotal"`
	WorkingDaysPassed  int     `json:"workingDaysPassed"`
	WorkingDaysRemain  int     `json:"workingDaysRemaining"`
}

type EpicData struct {
	EpicKey        string  `json:"epicKey"`
	EpicName       string  `json:"epicName"`
	Total          float64 `json:"total"`
	Completed      float64 `json:"completed"`
	Remaining      float64 `json:"remaining"`
	CompletionPct  float64 `json:"completionPct"`
	RiskStatus     string  `json:"riskStatus"`
	RiskReason     string  `json:"riskReason"`
	EpicOwner      string  `json:"epicOwner"`
	EpicOwnerEmail string  `json:"epicOwnerEmail"`
	AIRisk         string  `json:"aiRisk,omitempty"`
	AIInsight      string  `json:"aiInsight,omitempty"`
}

type PersonData struct {
	Name          string  `json:"name"`
	Assigned      float64 `json:"assigned"`
	Completed     float64 `json:"completed"`
	Remaining     float64 `json:"remaining"`
	CompletionPct float64 `json:"completionPct"`
}

type Summary struct {
	TotalPoints      float64      `json:"totalPoints"`
	CompletedPoints  float64      `json:"completedPoints"`
	InProgressPoints float64      `json:"inProgressPoints"`
	RemainingPoints  float64      `json:"remainingPoints"`
	CompletionPct    float64      `json:"completionPct"`
	PerPerson        []PersonData `json:"perPerson"`
	PerEpic          []EpicData   `json:"perEpic"`
	TimeProgress     TimeProgress `json:"timeProgress"`
}

type Forecast struct {
	DailyVelocity float64 `json:"dailyVelocity"`
	DaysNeeded    int     `json:"daysNeeded"`
	ForecastDelta int     `json:"forecastDelta"`
	Message       string  `json:"message"`
	Status        string  `json:"status"`
}

type HeatmapEntry struct {
	Name       string  `json:"name"`
	Total      float64 `json:"total"`
	Done       float64 `json:"done"`
	InProgress float64 `json:"inProgress"`
	Todo       float64 `json:"todo"`
	StuckCount int     `json:"stuckCount"`
	EpicCount  int     `json:"epicCount"`
	LoadFactor float64 `json:"loadFactor"`
	Status     string  `json:"status"`
}

type AlertItem struct {
	Key           string `json:"key"`
	Summary       string `json:"summary"`
	Assignee      string `json:"assignee"`
	AssigneeEmail string `json:"assigneeEmail,omitempty"`
	Points        float64 `json:"points,omitempty"`
	DaysStuck     int    `json:"daysStuck,omitempty"`
	Status        string `json:"status,omitempty"`
	AddedOn       string `json:"addedOn,omitempty"`
}

type ScopeCreep struct {
	Items      []AlertItem `json:"items"`
	TotalPts   float64     `json:"totalPoints"`
	PctOfSprint int        `json:"pctOfSprint"`
}

type ScoreCard struct {
	Committed     float64 `json:"committed"`
	Delivered     float64 `json:"delivered"`
	CommitmentPct int     `json:"commitmentPct"`
	Grade         string  `json:"grade"`
	ScopeCreepPct int     `json:"scopeCreepPct"`
	FocusFactor   int     `json:"focusFactor"`
	AgingItems    int     `json:"agingItems"`
	UnassignedPts float64 `json:"unassignedPoints"`
	CarryOver     int     `json:"carryOverCount"`
	SprintOver    bool    `json:"sprintOver"`
}

type Advanced struct {
	Forecast        Forecast       `json:"forecast"`
	CarryOverRisk   []AlertItem    `json:"carryOverRisk"`
	DailyThroughput []DayTP        `json:"dailyThroughput"`
	AgingWIP        []AlertItem    `json:"agingWIP"`
	AvailabilityGap AvailGap       `json:"availabilityGap"`
	FocusFactor     int            `json:"focusFactor"`
	ScopeCreep      ScopeCreep     `json:"scopeCreep"`
	Reopened        []AlertItem    `json:"reopened"`
	Unassigned      UnassignedData `json:"unassigned"`
	ScoreCard       ScoreCard      `json:"scoreCard"`
	CapacityHeatmap []HeatmapEntry `json:"capacityHeatmap"`
}

type DayTP struct {
	Date   string `json:"date"`
	Count  int    `json:"count"`
	Points float64 `json:"points"`
}

type AvailGap struct {
	RemainingWork float64 `json:"remainingWork"`
	EstCapacity   float64 `json:"estimatedCapacity"`
	Gap           float64 `json:"gap"`
	Status        string  `json:"status"`
	Message       string  `json:"message"`
}

type UnassignedData struct {
	Items    []AlertItem `json:"items"`
	TotalPts float64     `json:"totalPoints"`
}

type HealthResult struct {
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
	Score   int      `json:"score"`
}

type AIResult struct {
	Enabled         bool   `json:"enabled"`
	SprintInsight   string `json:"sprintInsight"`
	RetroPreview    string `json:"retroPreview"`
	ForecastInsight string `json:"forecastInsight"`
	RiskSummary     string `json:"riskSummary"`
}

type HistoryEntry struct {
	SprintName    string  `json:"sprintName"`
	Date          string  `json:"date"`
	TotalPoints   float64 `json:"totalPoints"`
	CompletedPts  float64 `json:"completedPoints"`
	CompletionPct float64 `json:"completionPct"`
	Velocity      float64 `json:"velocity"`
	ScopeCreepPct int     `json:"scopeCreepPct"`
	AgingItems    int     `json:"agingItems"`
	CarryOver     int     `json:"carryOver"`
	FocusFactor   int     `json:"focusFactor"`
	Grade         string  `json:"grade"`
}

// ---------- Helpers ----------

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func round1(f float64) float64 {
	return math.Round(f*10) / 10
}

// ---------- JIRA HTTP ----------

func jiraGet(path string) ([]byte, error) {
	if cached, ok := getCached(path); ok {
		return cached, nil
	}
	req, _ := http.NewRequest("GET", jiraBaseURL+path, nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("JIRA %d: %s", resp.StatusCode, string(body[:min(len(body), 150)]))
	}
	setCache(path, body)
	return body, nil
}

func jiraPost(path string, payload interface{}) ([]byte, error) {
	jsonBody, _ := json.Marshal(payload)
	cacheKey := path + string(jsonBody)
	if cached, ok := getCached(cacheKey); ok {
		return cached, nil
	}
	req, _ := http.NewRequest("POST", jiraBaseURL+path, bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("JIRA %d: %s", resp.StatusCode, string(body[:min(len(body), 150)]))
	}
	setCache(cacheKey, body)
	return body, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------- JIRA Data Functions ----------

func findBoard(projKey string) (int, error) {
	data, err := jiraGet(fmt.Sprintf("/rest/agile/1.0/board?projectKeyOrId=%s&maxResults=50", projKey))
	if err != nil {
		return 0, err
	}
	var result struct {
		Values []struct {
			ID   int    `json:"id"`
			Type string `json:"type"`
		} `json:"values"`
	}
	json.Unmarshal(data, &result)
	if len(result.Values) == 0 {
		return 0, fmt.Errorf("no board found for %s", projKey)
	}
	for _, b := range result.Values {
		if b.Type == "scrum" {
			return b.ID, nil
		}
	}
	return result.Values[0].ID, nil
}

func findSprint(boardID int, nameFragment string) (*Sprint, error) {
	fragment := strings.ToLower(nameFragment)
	for _, state := range []string{"active", "future", "closed"} {
		data, err := jiraGet(fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?state=%s&startAt=0&maxResults=50", boardID, state))
		if err != nil {
			continue
		}
		var result struct {
			Values []Sprint `json:"values"`
		}
		json.Unmarshal(data, &result)
		for _, s := range result.Values {
			if strings.Contains(strings.ToLower(s.Name), fragment) {
				return &s, nil
			}
		}
	}
	return nil, fmt.Errorf("sprint matching '%s' not found", nameFragment)
}

func getSprintIssues(sprintID int, projKey, team string) ([]Issue, error) {
	jql := fmt.Sprintf(`project = %s AND sprint = %d AND "team name[dropdown]" = "%s"`, projKey, sprintID, team)
	fields := []string{"summary", "status", "assignee", "customfield_10038", "customfield_10014", "issuetype", "created", "updated", "resolutiondate", "statuscategorychangedate"}
	var allIssues []Issue
	var nextPageToken string

	for {
		payload := map[string]interface{}{"jql": jql, "fields": fields, "maxResults": 100}
		if nextPageToken != "" {
			payload["nextPageToken"] = nextPageToken
		}
		data, err := jiraPost("/rest/api/3/search/jql", payload)
		if err != nil {
			return nil, err
		}
		var result struct {
			Issues        []json.RawMessage `json:"issues"`
			Total         int               `json:"total"`
			NextPageToken string            `json:"nextPageToken"`
		}
		json.Unmarshal(data, &result)
		for _, raw := range result.Issues {
			issue := parseIssue(raw)
			allIssues = append(allIssues, issue)
		}
		nextPageToken = result.NextPageToken
		if nextPageToken == "" || len(allIssues) >= result.Total {
			break
		}
	}
	log.Printf("  [issues] Fetched %d issues for %s/%s", len(allIssues), projKey, team)
	return allIssues, nil
}

func parseIssue(raw json.RawMessage) Issue {
	var data struct {
		Key    string `json:"key"`
		Fields struct {
			Summary  string `json:"summary"`
			Status   struct {
				Name           string `json:"name"`
				StatusCategory struct {
					Name string `json:"name"`
				} `json:"statusCategory"`
			} `json:"status"`
			Assignee *struct {
				DisplayName  string `json:"displayName"`
				EmailAddress string `json:"emailAddress"`
			} `json:"assignee"`
			StoryPoints            *float64 `json:"customfield_10038"`
			EpicLink               *string  `json:"customfield_10014"`
			IssueType              struct{ Name string `json:"name"` } `json:"issuetype"`
			Created                string `json:"created"`
			Updated                string `json:"updated"`
			ResolutionDate         *string `json:"resolutiondate"`
			StatusCategoryChangeDate *string `json:"statuscategorychangedate"`
		} `json:"fields"`
	}
	json.Unmarshal(raw, &data)

	assignee := "Unassigned"
	assigneeEmail := ""
	if data.Fields.Assignee != nil {
		assignee = data.Fields.Assignee.DisplayName
		assigneeEmail = data.Fields.Assignee.EmailAddress
	}
	sp := 0.0
	if data.Fields.StoryPoints != nil {
		sp = *data.Fields.StoryPoints
	}
	epicKey := ""
	if data.Fields.EpicLink != nil {
		epicKey = *data.Fields.EpicLink
	}
	resDate := ""
	if data.Fields.ResolutionDate != nil {
		resDate = *data.Fields.ResolutionDate
	}
	scDate := ""
	if data.Fields.StatusCategoryChangeDate != nil {
		scDate = *data.Fields.StatusCategoryChangeDate
	}

	return Issue{
		Key:              data.Key,
		Summary:          data.Fields.Summary,
		Status:           data.Fields.Status.Name,
		StatusCategory:   data.Fields.Status.StatusCategory.Name,
		Assignee:         assignee,
		AssigneeEmail:    assigneeEmail,
		StoryPoints:      sp,
		EpicKey:          epicKey,
		EpicName:         epicKey,
		IssueType:        data.Fields.IssueType.Name,
		Created:          data.Fields.Created,
		Updated:          data.Fields.Updated,
		ResolutionDate:   resDate,
		StatusChangeDate: scDate,
	}
}

// ---------- Analytics ----------

func countWorkingDays(from, to time.Time) int {
	count := 0
	d := from
	for d.Before(to) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			count++
		}
		d = d.AddDate(0, 0, 1)
	}
	return count
}

func computeTimeProgress(startStr, endStr string) TimeProgress {
	now := time.Now().Truncate(24 * time.Hour)
	start, err1 := time.Parse(time.RFC3339, startStr)
	end, err2 := time.Parse(time.RFC3339, endStr)
	if err1 != nil || err2 != nil {
		// Try date-only format
		start, err1 = time.Parse("2006-01-02", startStr[:min(len(startStr), 10)])
		end, err2 = time.Parse("2006-01-02", endStr[:min(len(endStr), 10)])
		if err1 != nil || err2 != nil {
			return TimeProgress{}
		}
	}
	start = start.Truncate(24 * time.Hour)
	end = end.Truncate(24 * time.Hour)

	totalDays := int(end.Sub(start).Hours() / 24)
	if totalDays <= 0 {
		totalDays = 1
	}
	daysPassed := int(now.Sub(start).Hours() / 24)
	if daysPassed < 0 {
		daysPassed = 0
	}
	if daysPassed > totalDays {
		daysPassed = totalDays
	}

	effectiveNow := now
	if now.After(end) {
		effectiveNow = end
	}
	wdTotal := countWorkingDays(start, end)
	if wdTotal == 0 {
		wdTotal = 1
	}
	wdPassed := countWorkingDays(start, effectiveNow)
	wdRemain := wdTotal - wdPassed
	pct := round1(float64(wdPassed) / float64(wdTotal) * 100)

	return TimeProgress{
		TotalDays:         totalDays,
		DaysPassed:        daysPassed,
		DaysRemaining:     totalDays - daysPassed,
		TimeElapsedPct:    pct,
		WorkingDaysTotal:  wdTotal,
		WorkingDaysPassed: wdPassed,
		WorkingDaysRemain: wdRemain,
	}
}

func computeSummary(issues []Issue, tp TimeProgress) Summary {
	var total, completed, inProgress float64
	for _, i := range issues {
		total += i.StoryPoints
		if i.StatusCategory == "Done" {
			completed += i.StoryPoints
		} else if i.StatusCategory == "In Progress" {
			inProgress += i.StoryPoints
		}
	}
	pct := 0.0
	if total > 0 {
		pct = round1(completed / total * 100)
	}
	return Summary{
		TotalPoints:      total,
		CompletedPoints:  completed,
		InProgressPoints: inProgress,
		RemainingPoints:  total - completed,
		CompletionPct:    pct,
		PerPerson:        computePerPerson(issues),
		PerEpic:          computePerEpic(issues, tp),
		TimeProgress:     tp,
	}
}

func computePerPerson(issues []Issue) []PersonData {
	people := map[string]*PersonData{}
	for _, i := range issues {
		p, ok := people[i.Assignee]
		if !ok {
			p = &PersonData{Name: i.Assignee}
			people[i.Assignee] = p
		}
		p.Assigned += i.StoryPoints
		if i.StatusCategory == "Done" {
			p.Completed += i.StoryPoints
		} else {
			p.Remaining += i.StoryPoints
		}
	}
	var result []PersonData
	for _, p := range people {
		if p.Assigned > 0 {
			p.CompletionPct = round1(p.Completed / p.Assigned * 100)
		}
		result = append(result, *p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Assigned > result[j].Assigned })
	if result == nil { result = []PersonData{} }
	return result
}

func computePerEpic(issues []Issue, tp TimeProgress) []EpicData {
	type epicAcc struct {
		EpicData
		contributors map[string]float64
		emails       map[string]string
	}
	epics := map[string]*epicAcc{}
	for _, i := range issues {
		key := i.EpicKey
		if key == "" {
			key = "No Epic"
		}
		e, ok := epics[key]
		if !ok {
			e = &epicAcc{EpicData: EpicData{EpicKey: key, EpicName: key}, contributors: map[string]float64{}, emails: map[string]string{}}
			epics[key] = e
		}
		e.Total += i.StoryPoints
		if i.StatusCategory == "Done" {
			e.Completed += i.StoryPoints
		} else {
			e.Remaining += i.StoryPoints
		}
		if i.Assignee != "Unassigned" {
			e.contributors[i.Assignee] += i.StoryPoints
			if i.AssigneeEmail != "" {
				e.emails[i.Assignee] = i.AssigneeEmail
			}
		}
	}

	timePct := tp.TimeElapsedPct / 100
	var result []EpicData
	for _, e := range epics {
		risk := calculateEpicRisk(e.Total, e.Completed, e.Remaining, timePct)
		e.RiskStatus = risk[0]
		e.RiskReason = risk[1]
		if e.Total > 0 {
			e.CompletionPct = round1(e.Completed / e.Total * 100)
		}
		// Find top contributor as owner
		var topName string
		var topPts float64
		for name, pts := range e.contributors {
			if pts > topPts {
				topName = name
				topPts = pts
			}
		}
		if topName != "" {
			e.EpicOwner = topName
			e.EpicOwnerEmail = e.emails[topName]
		} else {
			e.EpicOwner = "Unassigned"
		}
		result = append(result, e.EpicData)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Total > result[j].Total })
	return result
}

func calculateEpicRisk(total, completed, remaining, timeElapsed float64) [2]string {
	if total == 0 {
		return [2]string{"On Track", "No story points assigned"}
	}
	workProgress := completed / total
	remainingTime := 1.0 - timeElapsed
	if workProgress >= 1 {
		return [2]string{"On Track", "Epic fully completed"}
	}
	if timeElapsed == 0 {
		return [2]string{"On Track", "Sprint just started"}
	}
	velocityRatio := workProgress / timeElapsed
	if remainingTime <= 0 {
		return [2]string{"Not Deliverable", fmt.Sprintf("Sprint ended with %.0f points remaining", remaining)}
	}
	if velocityRatio >= 0.8 {
		return [2]string{"On Track", fmt.Sprintf("%.0f%% done vs %.0f%% time elapsed", workProgress*100, timeElapsed*100)}
	}
	requiredAccel := (remaining / total) / remainingTime
	if velocityRatio >= 0.5 && remainingTime > 0.3 {
		return [2]string{"At Risk", fmt.Sprintf("Only %.0f%% done with %.0f%% time elapsed. Needs %.1fx acceleration.", workProgress*100, timeElapsed*100, requiredAccel)}
	}
	return [2]string{"Not Deliverable", fmt.Sprintf("Only %.0f%% done with %.0f%% time elapsed. %.0f pts remaining in %.0f%% of sprint.", workProgress*100, timeElapsed*100, remaining, remainingTime*100)}
}

func assessSprintHealth(s Summary, tp TimeProgress) HealthResult {
	score := 100
	var reasons []string
	timePct := tp.TimeElapsedPct / 100
	if s.TotalPoints > 0 && timePct > 0 {
		workPct := s.CompletedPoints / s.TotalPoints
		ratio := workPct / timePct
		if ratio < 0.5 {
			score -= 40
			reasons = append(reasons, fmt.Sprintf("Work severely behind: %.0f%% done vs %.0f%% time", workPct*100, timePct*100))
		} else if ratio < 0.75 {
			score -= 20
			reasons = append(reasons, fmt.Sprintf("Work behind schedule: %.0f%% done vs %.0f%% time", workPct*100, timePct*100))
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "Sprint progressing well")
	}
	status := "Healthy"
	if score < 70 {
		status = "At Risk"
	}
	if score < 40 {
		status = "Unhealthy"
	}
	return HealthResult{Status: status, Reasons: reasons, Score: max(score, 0)}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------- History ----------

func getHistoryPath() string {
	return filepath.Join(filepath.Dir(os.Args[0]), "..", "data", "sprint-history.json")
}

func saveHistory(sprintN string, s Summary, adv Advanced, proj, team string) {
	path := getHistoryPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	today := time.Now().Format("2006-01-02")
	entry := HistoryEntry{
		SprintName:    sprintN,
		Date:          today,
		TotalPoints:   s.TotalPoints,
		CompletedPts:  s.CompletedPoints,
		CompletionPct: s.CompletionPct,
		Velocity:      adv.Forecast.DailyVelocity,
		ScopeCreepPct: adv.ScopeCreep.PctOfSprint,
		AgingItems:    len(adv.AgingWIP),
		CarryOver:     len(adv.CarryOverRisk),
		FocusFactor:   adv.FocusFactor,
		Grade:         adv.ScoreCard.Grade,
	}

	key := proj + "_" + team
	history := map[string][]HistoryEntry{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &history)
	}
	entries := history[key]
	found := false
	for i, e := range entries {
		if e.SprintName == sprintN && e.Date == today {
			entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, entry)
	}
	if len(entries) > 100 {
		entries = entries[len(entries)-100:]
	}
	history[key] = entries
	data, _ := json.MarshalIndent(history, "", "  ")
	os.WriteFile(path, data, 0644)
}

func getHistory(proj, team string) []HistoryEntry {
	path := getHistoryPath()
	history := map[string][]HistoryEntry{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &history)
	}
	return history[proj+"_"+team]
}

// ---------- Advanced Metrics ----------

func computeAdvanced(issues []Issue, tp TimeProgress, startStr, endStr string) Advanced {
	if issues == nil {
		issues = []Issue{}
	}
	now := time.Now()
	start, err1 := time.Parse(time.RFC3339, startStr)
	if err1 != nil {
		start, _ = time.Parse("2006-01-02", startStr[:min(len(startStr), 10)])
	}
	end, err2 := time.Parse(time.RFC3339, endStr)
	if err2 != nil {
		end, _ = time.Parse("2006-01-02", endStr[:min(len(endStr), 10)])
	}
	start = start.Truncate(24 * time.Hour)
	end = end.Truncate(24 * time.Hour)

	var totalPts, completedPts float64
	for _, i := range issues {
		totalPts += i.StoryPoints
		if i.StatusCategory == "Done" {
			completedPts += i.StoryPoints
		}
	}
	wdPassed := max(tp.WorkingDaysPassed, 1)
	dailyVel := completedPts / float64(wdPassed)
	remainPts := totalPts - completedPts
	daysNeeded := 999
	if dailyVel > 0 {
		daysNeeded = int(math.Ceil(remainPts / dailyVel))
	}
	delta := tp.WorkingDaysRemain - daysNeeded
	fMsg := fmt.Sprintf("At current velocity, sprint will be %d day(s) late", -delta)
	fStatus := "behind"
	if delta >= 0 {
		fMsg = fmt.Sprintf("On pace to finish %d working day(s) early", delta)
		fStatus = "on-track"
	} else if delta >= -2 {
		fStatus = "at-risk"
	}

	forecast := Forecast{DailyVelocity: round1(dailyVel), DaysNeeded: daysNeeded, ForecastDelta: delta, Message: fMsg, Status: fStatus}

	// Aging WIP
	var agingWIP []AlertItem
	for _, i := range issues {
		if i.StatusCategory != "In Progress" {
			continue
		}
		var lastChange time.Time
		if i.StatusChangeDate != "" {
			lastChange, _ = time.Parse(time.RFC3339, i.StatusChangeDate)
		} else if i.Updated != "" {
			lastChange, _ = time.Parse(time.RFC3339, i.Updated)
		}
		if lastChange.IsZero() {
			continue
		}
		days := int(now.Sub(lastChange).Hours() / 24)
		if days >= 3 {
			agingWIP = append(agingWIP, AlertItem{Key: i.Key, Summary: i.Summary, Assignee: i.Assignee, AssigneeEmail: i.AssigneeEmail, Points: i.StoryPoints, DaysStuck: days})
		}
	}
	sort.Slice(agingWIP, func(i, j int) bool { return agingWIP[i].DaysStuck > agingWIP[j].DaysStuck })

	// Carry-Over Risk
	var carryOver []AlertItem
	for _, i := range issues {
		if i.StatusCategory == "Done" {
			continue
		}
		if (i.StatusCategory == "To Do" && tp.WorkingDaysRemain <= 2) || (i.StoryPoints >= 5 && i.StatusCategory == "To Do" && tp.WorkingDaysRemain <= 3) {
			carryOver = append(carryOver, AlertItem{Key: i.Key, Summary: i.Summary, Points: i.StoryPoints, Status: i.Status, Assignee: i.Assignee})
		}
	}

	// Scope Creep
	sprintStart := start.Format("2006-01-02")
	var scopeItems []AlertItem
	var scopePts float64
	for _, i := range issues {
		if i.Created != "" && i.Created[:min(len(i.Created), 10)] > sprintStart {
			scopeItems = append(scopeItems, AlertItem{Key: i.Key, Summary: i.Summary, Points: i.StoryPoints, AddedOn: i.Created[:min(len(i.Created), 10)], Assignee: i.Assignee})
			scopePts += i.StoryPoints
		}
	}
	scopePct := 0
	if totalPts > 0 {
		scopePct = int(math.Round(scopePts / totalPts * 100))
	}

	// Focus Factor
	var planned float64
	for _, i := range issues {
		if i.IssueType == "Story" || i.IssueType == "Task" || i.IssueType == "Sub-task" {
			planned += i.StoryPoints
		}
	}
	focusFactor := 100
	if totalPts > 0 {
		focusFactor = int(math.Round(planned / totalPts * 100))
	}

	// Unassigned
	var unassigned []AlertItem
	var unassignedPts float64
	for _, i := range issues {
		if i.Assignee == "Unassigned" && i.StatusCategory != "Done" {
			unassigned = append(unassigned, AlertItem{Key: i.Key, Summary: i.Summary, Points: i.StoryPoints, Status: i.Status})
			unassignedPts += i.StoryPoints
		}
	}

	// Re-opened
	var reopened []AlertItem
	for _, i := range issues {
		if i.ResolutionDate != "" && i.StatusCategory != "Done" {
			reopened = append(reopened, AlertItem{Key: i.Key, Summary: i.Summary, Assignee: i.Assignee})
		}
	}

	// Availability Gap
	remCap := dailyVel * float64(tp.WorkingDaysRemain)
	gap := round1(remCap - remainPts)
	gapStatus := "under-committed"
	gapMsg := fmt.Sprintf("Team has capacity for %.0f more points", remCap-remainPts)
	if remCap < remainPts {
		gapStatus = "over-committed"
		gapMsg = fmt.Sprintf("Team is over-committed by %.0f points", remainPts-remCap)
	}

	// Daily Throughput
	var throughput []DayTP
	d := start
	for !d.After(now) && !d.After(end) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			dayStr := d.Format("2006-01-02")
			var cnt int
			var pts float64
			for _, i := range issues {
				if i.ResolutionDate != "" && strings.HasPrefix(i.ResolutionDate, dayStr) {
					cnt++
					pts += i.StoryPoints
				}
			}
			throughput = append(throughput, DayTP{Date: dayStr, Count: cnt, Points: pts})
		}
		d = d.AddDate(0, 0, 1)
	}

	// Score Card
	commitPct := 0
	if totalPts > 0 {
		commitPct = int(math.Round(completedPts / totalPts * 100))
	}
	grade := "A"
	if commitPct < 90 {
		grade = "B"
	}
	if commitPct < 75 {
		grade = "C"
	}
	if commitPct < 50 {
		grade = "D"
	}
	sprintOver := now.After(end)
	if !sprintOver {
		grade = "-"
	}

	// Capacity Heatmap
	heatmap := computeHeatmap(issues, agingWIP, totalPts)

	// Ensure no nil slices (Go nil → JSON null breaks frontend)
	if carryOver == nil { carryOver = []AlertItem{} }
	if throughput == nil { throughput = []DayTP{} }
	if agingWIP == nil { agingWIP = []AlertItem{} }
	if scopeItems == nil { scopeItems = []AlertItem{} }
	if reopened == nil { reopened = []AlertItem{} }
	if unassigned == nil { unassigned = []AlertItem{} }

	return Advanced{
		Forecast:        forecast,
		CarryOverRisk:   carryOver,
		DailyThroughput: throughput,
		AgingWIP:        agingWIP,
		AvailabilityGap: AvailGap{RemainingWork: remainPts, EstCapacity: round1(remCap), Gap: gap, Status: gapStatus, Message: gapMsg},
		FocusFactor:     focusFactor,
		ScopeCreep:      ScopeCreep{Items: scopeItems, TotalPts: scopePts, PctOfSprint: scopePct},
		Reopened:        reopened,
		Unassigned:      UnassignedData{Items: unassigned, TotalPts: unassignedPts},
		ScoreCard:       ScoreCard{Committed: totalPts, Delivered: completedPts, CommitmentPct: commitPct, Grade: grade, ScopeCreepPct: scopePct, FocusFactor: focusFactor, AgingItems: len(agingWIP), UnassignedPts: unassignedPts, CarryOver: len(carryOver), SprintOver: sprintOver},
		CapacityHeatmap: heatmap,
	}
}

func computeHeatmap(issues []Issue, agingWIP []AlertItem, totalPts float64) []HeatmapEntry {
	type person struct {
		name       string
		total, done, wip, todo float64
		stuckCount int
		epics      map[string]bool
	}
	people := map[string]*person{}
	for _, i := range issues {
		if i.Assignee == "Unassigned" {
			continue
		}
		p, ok := people[i.Assignee]
		if !ok {
			p = &person{name: i.Assignee, epics: map[string]bool{}}
			people[i.Assignee] = p
		}
		p.total += i.StoryPoints
		switch i.StatusCategory {
		case "Done":
			p.done += i.StoryPoints
		case "In Progress":
			p.wip += i.StoryPoints
		default:
			p.todo += i.StoryPoints
		}
		if i.EpicKey != "" {
			p.epics[i.EpicKey] = true
		}
	}
	for _, a := range agingWIP {
		if p, ok := people[a.Assignee]; ok {
			p.stuckCount++
		}
	}

	uniquePeople := len(people)
	if uniquePeople == 0 {
		uniquePeople = 1
	}
	avg := totalPts / float64(uniquePeople)

	var result []HeatmapEntry
	for _, p := range people {
		load := 0.0
		if avg > 0 {
			load = p.total / avg
		}
		status := "balanced"
		if load > 1.5 {
			status = "overloaded"
		} else if load > 1.2 {
			status = "high"
		} else if load < 0.5 {
			status = "low"
		}
		result = append(result, HeatmapEntry{
			Name: p.name, Total: p.total, Done: p.done, InProgress: p.wip, Todo: p.todo,
			StuckCount: p.stuckCount, EpicCount: len(p.epics),
			LoadFactor: math.Round(load*100) / 100, Status: status,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LoadFactor > result[j].LoadFactor })
	if result == nil {
		result = []HeatmapEntry{}
	}
	return result
}

// ---------- HTTP Handlers ----------

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

func handleDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"projectKey": projectKey, "teamName": teamName, "sprintName": sprintName, "jiraBaseUrl": jiraBaseURL})
}

func handleAll(w http.ResponseWriter, r *http.Request) {
	proj := queryOr(r, "project", projectKey)
	team := queryOr(r, "team", teamName)
	sprintN := queryOr(r, "sprint", sprintName)

	boardID, err := findBoard(proj)
	if err != nil {
		writeJSON(w, map[string]interface{}{"empty": true, "message": fmt.Sprintf("Board not found for project %s", proj)})
		return
	}
	sprint, err := findSprint(boardID, sprintN)
	if err != nil {
		writeJSON(w, map[string]interface{}{"empty": true, "message": fmt.Sprintf("Sprint not found: %s", err.Error()), "sprint": map[string]interface{}{"name": sprintN, "teamName": team, "jiraBaseUrl": jiraBaseURL}})
		return
	}
	issues, err := getSprintIssues(sprint.ID, proj, team)
	if err != nil {
		writeJSON(w, map[string]interface{}{"empty": true, "message": fmt.Sprintf("Error fetching issues: %s", err.Error()), "sprint": map[string]interface{}{"name": sprint.Name, "teamName": team, "jiraBaseUrl": jiraBaseURL}})
		return
	}
	if issues == nil {
		issues = []Issue{}
	}
	if len(issues) == 0 {
		writeJSON(w, map[string]interface{}{"empty": true, "message": fmt.Sprintf("%s has no issues in %s", team, sprint.Name), "sprint": map[string]interface{}{"name": sprint.Name, "teamName": team, "jiraBaseUrl": jiraBaseURL}})
		return
	}

	tp := computeTimeProgress(sprint.StartDate, sprint.EndDate)
	summary := computeSummary(issues, tp)
	health := assessSprintHealth(summary, tp)
	advanced := computeAdvanced(issues, tp, sprint.StartDate, sprint.EndDate)

	saveHistory(sprint.Name, summary, advanced, proj, team)
	history := getHistory(proj, team)

	// AI-powered risk analysis via Bedrock Claude
	aiResult := AIResult{Enabled: isAIEnabled()}
	if aiResult.Enabled {
		epicSummary := ""
		for _, e := range summary.PerEpic {
			epicSummary += fmt.Sprintf("- %s: %g/%g pts done (%.0f%%), risk: %s\n", e.EpicName, e.Completed, e.Total, e.CompletionPct, e.RiskStatus)
		}
		prompt := fmt.Sprintf(`You are a senior Agile delivery coach. Analyze this sprint:

SPRINT: %s | Team: %s | Time elapsed: %.0f%% | Working days left: %d

EPICS:
%s
METRICS: Velocity %.1f pts/day, Scope creep %d%%, Focus factor %d%%, Aging items %d

Provide a JSON response with:
{"sprintInsight": "2-3 sentence health summary", "retroPreview": "What's going well + what needs attention (2 bullets each)", "riskSummary": "one sentence biggest risk"}

Respond ONLY with valid JSON, no markdown.`, sprint.Name, team, tp.TimeElapsedPct, tp.WorkingDaysRemain, epicSummary, advanced.Forecast.DailyVelocity, advanced.ScopeCreep.PctOfSprint, advanced.FocusFactor, len(advanced.AgingWIP))

		aiText, err := callAI(prompt)
		if err == nil && aiText != "" {
			// Extract JSON from response (handle markdown fences, nested objects, etc.)
			jsonMatch := aiText
			if idx := strings.Index(aiText, "{"); idx >= 0 {
				if end := strings.LastIndex(aiText, "}"); end > idx {
					jsonMatch = aiText[idx : end+1]
				}
			}
			// Try parsing as map[string]interface{} to handle any value type
			var parsed map[string]interface{}
			if parseErr := json.Unmarshal([]byte(jsonMatch), &parsed); parseErr == nil {
				if v, ok := parsed["sprintInsight"]; ok {
					aiResult.SprintInsight = fmt.Sprintf("%v", v)
				}
				if v, ok := parsed["retroPreview"]; ok {
					aiResult.RetroPreview = fmt.Sprintf("%v", v)
				}
				if v, ok := parsed["riskSummary"]; ok {
					aiResult.RiskSummary = fmt.Sprintf("%v", v)
				}
				log.Printf("  [AI] Analysis complete: insight=%d chars", len(aiResult.SprintInsight))
			} else {
				// If JSON parse fails, use entire text as sprint insight
				aiResult.SprintInsight = strings.TrimSpace(aiText)
				log.Printf("  [AI] JSON parse failed, using raw text: %s", parseErr)
			}
		} else if err != nil {
			log.Printf("  [AI] Failed: %s", err)
		}
	}

	resp := map[string]interface{}{
		"sprint":   map[string]interface{}{"id": sprint.ID, "name": sprint.Name, "state": sprint.State, "startDate": sprint.StartDate, "endDate": sprint.EndDate, "teamName": team, "projectKey": proj, "jiraBaseUrl": jiraBaseURL},
		"summary":  summary,
		"health":   health,
		"issues":   issues,
		"advanced": advanced,
		"history":  history,
		"ai":       aiResult,
	}
	writeJSON(w, resp)
}

func handleSprints(w http.ResponseWriter, r *http.Request) {
	proj := queryOr(r, "project", projectKey)
	boardID, err := findBoard(proj)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var sprints []Sprint
	for _, state := range []string{"active", "future"} {
		data, err := jiraGet(fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?state=%s&startAt=0&maxResults=50", boardID, state))
		if err != nil {
			continue
		}
		var result struct{ Values []Sprint `json:"values"` }
		json.Unmarshal(data, &result)
		sprints = append(sprints, result.Values...)
	}
	writeJSON(w, map[string]interface{}{"sprints": sprints})
}

func handleTeams(w http.ResponseWriter, r *http.Request) {
	proj := queryOr(r, "project", projectKey)
	// Find team field ID
	data, err := jiraGet("/rest/api/3/field")
	if err != nil {
		writeJSON(w, map[string]interface{}{"teams": []string{}})
		return
	}
	var fields []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		ClauseNames []string `json:"clauseNames"`
	}
	json.Unmarshal(data, &fields)
	var teamFieldID string
	for _, f := range fields {
		for _, c := range f.ClauseNames {
			if strings.Contains(strings.ToLower(c), "team name") {
				teamFieldID = f.ID
				break
			}
		}
		if teamFieldID != "" {
			break
		}
	}
	if teamFieldID == "" {
		writeJSON(w, map[string]interface{}{"teams": []string{}})
		return
	}

	// Search for issues with team field set
	payload := map[string]interface{}{
		"jql":        fmt.Sprintf(`project = %s AND "%s" IS NOT EMPTY ORDER BY updated DESC`, proj, "team name[dropdown]"),
		"fields":     []string{teamFieldID},
		"maxResults": 100,
	}
	searchData, err := jiraPost("/rest/api/3/search/jql", payload)
	if err != nil {
		writeJSON(w, map[string]interface{}{"teams": []string{}})
		return
	}
	var searchResult struct {
		Issues []struct {
			Fields map[string]interface{} `json:"fields"`
		} `json:"issues"`
	}
	json.Unmarshal(searchData, &searchResult)

	teams := map[string]bool{}
	for _, issue := range searchResult.Issues {
		val := issue.Fields[teamFieldID]
		if val == nil {
			continue
		}
		switch v := val.(type) {
		case string:
			teams[v] = true
		case map[string]interface{}:
			if name, ok := v["value"].(string); ok {
				teams[name] = true
			} else if name, ok := v["name"].(string); ok {
				teams[name] = true
			}
		}
	}
	var teamList []string
	for t := range teams {
		teamList = append(teamList, t)
	}
	sort.Strings(teamList)
	writeJSON(w, map[string]interface{}{"teams": teamList})
}

func handleRefresh(w http.ResponseWriter, r *http.Request) {
	clearCache()
	writeJSON(w, map[string]string{"status": "cache cleared"})
}

// ---------- AWS Credentials (reads from ~/.aws/credentials) ----------

type awsCreds struct {
	AccessKey    string
	SecretKey    string
	SessionToken string
}

func getAWSCreds() (*awsCreds, error) {
	// First try env vars
	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	st := os.Getenv("AWS_SESSION_TOKEN")
	if ak != "" && sk != "" {
		return &awsCreds{AccessKey: ak, SecretKey: sk, SessionToken: st}, nil
	}

	// Read from ~/.aws/credentials file
	home, _ := os.UserHomeDir()
	credFile := filepath.Join(home, ".aws", "credentials")
	data, err := os.ReadFile(credFile)
	if err != nil {
		return nil, fmt.Errorf("no AWS credentials found (env vars or %s)", credFile)
	}

	lines := strings.Split(string(data), "\n")
	profile := "[" + awsProfile + "]"
	inProfile := false
	creds := &awsCreds{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == profile {
			inProfile = true
			continue
		}
		if strings.HasPrefix(line, "[") && inProfile {
			break // hit next profile
		}
		if inProfile {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			switch key {
			case "aws_access_key_id":
				creds.AccessKey = val
			case "aws_secret_access_key":
				creds.SecretKey = val
			case "aws_session_token":
				creds.SessionToken = val
			}
		}
	}

	if creds.AccessKey == "" || creds.SecretKey == "" {
		return nil, fmt.Errorf("profile [%s] not found or incomplete in %s", awsProfile, credFile)
	}
	return creds, nil
}

func isAIEnabled() bool {
	creds, err := getAWSCreds()
	if err == nil && creds.AccessKey != "" {
		return true
	}
	return os.Getenv("GITHUB_TOKEN") != "" || os.Getenv("GH_MODELS_TOKEN") != ""
}

// ---------- AWS Bedrock (Claude) ----------

func awsSign(method, host, path, region, service string, body []byte, creds *awsCreds) *http.Request {
	t := time.Now().UTC()
	datestamp := t.Format("20060102")
	amzdate := t.Format("20060102T150405Z")
	bodyHash := sha256Hex(body)

	// Build canonical headers (must be sorted)
	canonicalHeaders := fmt.Sprintf("content-type:application/json\nhost:%s\nx-amz-date:%s\n", host, amzdate)
	signedHeaders := "content-type;host;x-amz-date"
	if creds.SessionToken != "" {
		canonicalHeaders = fmt.Sprintf("content-type:application/json\nhost:%s\nx-amz-date:%s\nx-amz-security-token:%s\n", host, amzdate, creds.SessionToken)
		signedHeaders = "content-type;host;x-amz-date;x-amz-security-token"
	}

	canonicalRequest := fmt.Sprintf("%s\n%s\n\n%s\n%s\n%s", method, path, canonicalHeaders, signedHeaders, bodyHash)

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", datestamp, region, service)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", amzdate, credentialScope, sha256Hex([]byte(canonicalRequest)))

	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256([]byte("AWS4"+creds.SecretKey), datestamp), region), service), "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	authorizationHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", creds.AccessKey, credentialScope, signedHeaders, signature)

	req, _ := http.NewRequest(method, "https://"+host+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Amz-Date", amzdate)
	req.Header.Set("Authorization", authorizationHeader)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}
	return req
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// callAI tries Bedrock first, falls back to GitHub Models (free GPT-4o-mini)
func callAI(prompt string) (string, error) {
	// Try Bedrock first
	creds, err := getAWSCreds()
	if err == nil {
		text, err := callBedrockDirect(prompt, creds)
		if err == nil {
			return text, nil
		}
		log.Printf("  [AI] Bedrock failed: %s, trying GitHub Models fallback...", err)
	}

	// Fallback: GitHub Models (free GPT-4o-mini)
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		githubToken = os.Getenv("GH_MODELS_TOKEN") // Codespaces can't use GITHUB_ prefix
	}
	if githubToken == "" {
		return "", fmt.Errorf("no AI provider available (Bedrock creds missing, GITHUB_TOKEN/GH_MODELS_TOKEN not set)")
	}
	return callGitHubModelsFree(prompt, githubToken)
}

func callBedrockDirect(prompt string, creds *awsCreds) (string, error) {
	payload := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "user", "content": []map[string]interface{}{{"text": prompt}}},
		},
		"inferenceConfig": map[string]interface{}{"maxTokens": 1500, "temperature": 0.3},
	}
	body, _ := json.Marshal(payload)

	host := fmt.Sprintf("bedrock-runtime.%s.amazonaws.com", awsRegion)
	path := fmt.Sprintf("/model/%s/converse", bedrockModel)
	req := awsSign("POST", host, path, awsRegion, "bedrock", body, creds)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bedrock request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("bedrock %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 100)]))
	}

	var result struct {
		Output struct {
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
	}
	json.Unmarshal(respBody, &result)
	if len(result.Output.Message.Content) == 0 {
		return "", fmt.Errorf("empty bedrock response")
	}
	log.Printf("  [AI] Bedrock (Claude) response: %d chars", len(result.Output.Message.Content[0].Text))
	return result.Output.Message.Content[0].Text, nil
}

func callGitHubModelsFree(prompt, token string) (string, error) {
	payload := map[string]interface{}{
		"model":       "gpt-4o-mini",
		"messages":    []map[string]interface{}{{"role": "user", "content": prompt}},
		"temperature": 0.3,
		"max_tokens":  1500,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "https://models.inference.ai.azure.com/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github models failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("github models %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 100)]))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(respBody, &result)
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty github models response")
	}
	log.Printf("  [AI] GitHub Models (GPT-4o-mini) response: %d chars", len(result.Choices[0].Message.Content))
	return result.Choices[0].Message.Content, nil
}

var teamsWittyMessages = map[string][]string{
	"epic": {
		"This epic is giving 'I'll finish it next sprint' energy. 🙃",
		"Houston, we have an epic problem. 🚀💥",
		"This epic aged like milk, not wine. 🧀",
		"Someone call 911 — this epic needs emergency care. 🚑",
		"This epic is moving slower than Monday morning standup. ☕",
		"Plot twist: the epic decided it belongs in another sprint. 📖",
	},
	"stuck": {
		"This ticket hasn't moved since dinosaurs roamed the earth. 🦕",
		"Is this ticket on vacation? Asking for the sprint goal. 🏖️",
		"This item is stuck longer than my last Windows update. 💻",
		"Day 5+: Still waiting. Send snacks. 🍕",
		"This ticket is collecting dust like my gym membership. 🏋️",
		"Knock knock. Who's there? Not this ticket — it's stuck. 🚪",
	},
	"forecast": {
		"At this velocity, we'll finish... eventually. ⏳",
		"Sprint goal watching the team like: 👀",
		"The burndown chart is more of a flat line at this point. 📉",
		"We're behind schedule. Time to cancel some meetings. 📅",
	},
	"scope": {
		"Scope creep alert! Someone's been sneaking items in. 🛒",
		"The backlog is growing faster than a Slack thread. 💬",
		"Who ordered extra scope? Nobody? Thought so. 🤷",
		"Sprint scope expanding like a dev's estimate of '2 hours'. ⏰",
	},
	"unassigned": {
		"These orphan tickets need a loving developer. 🥺",
		"Free tickets! No takers? Anyone? Bueller? 🎬",
		"These tickets are like gym memberships — nobody's showing up. 🏋️",
	},
}

func getTeamsWitty(category string) string {
	msgs := teamsWittyMessages[category]
	if len(msgs) == 0 {
		msgs = []string{"Attention needed! Time to take action. 👀"}
	}
	return msgs[time.Now().UnixNano()%int64(len(msgs))]
}

type mention struct {
	Name  string
	Email string
}

func sendTeamsNotification(title string, facts []map[string]string, mentions ...mention) {
	if teamsWebhookURL == "" {
		return
	}

	// Determine category for witty message
	category := "epic"
	titleLower := strings.ToLower(title)
	if strings.Contains(titleLower, "stuck") {
		category = "stuck"
	} else if strings.Contains(titleLower, "forecast") || strings.Contains(titleLower, "behind") {
		category = "forecast"
	} else if strings.Contains(titleLower, "scope") {
		category = "scope"
	} else if strings.Contains(titleLower, "unassigned") {
		category = "unassigned"
	}
	witty := getTeamsWitty(category)

	// Determine severity color
	alertColor := "Accent"
	emoji := "🔵"
	if strings.Contains(titleLower, "not deliverable") || strings.Contains(titleLower, "stuck") {
		alertColor = "Attention"
		emoji = "🔴"
	} else if strings.Contains(titleLower, "risk") || strings.Contains(titleLower, "behind") || strings.Contains(titleLower, "scope") {
		alertColor = "Warning"
		emoji = "🟠"
	}

	factItems := make([]interface{}, 0)
	for _, f := range facts {
		factItems = append(factItems, map[string]string{"title": f["name"], "value": "**" + f["value"] + "**"})
	}

	// Find person to mention
	mentionName := ""
	mentionEmail := ""
	for _, m := range mentions {
		if m.Name != "" && m.Name != "Unassigned" {
			mentionName = m.Name
			mentionEmail = m.Email
			break
		}
	}
	if mentionName == "" {
		for _, f := range facts {
			if (f["name"] == "Owner" || f["name"] == "Assignee") && f["value"] != "" && f["value"] != "Unassigned" {
				mentionName = f["value"]
				mentionEmail = strings.ToLower(strings.ReplaceAll(f["value"], " ", ".")) + "@nice.com"
				break
			}
		}
	}

	// Build action text
	actionText := "⚡ **Action Required** — Please review and take action."
	if mentionName != "" {
		actionText = "⚡ <at>" + mentionName + "</at> — please take action!"
	}

	// Build body
	bodyItems := []interface{}{
		map[string]interface{}{"type": "TextBlock", "text": emoji + " SPRINT ALERT", "size": "Small", "weight": "Bolder", "color": alertColor},
		map[string]interface{}{"type": "TextBlock", "text": title, "size": "Large", "weight": "Bolder", "wrap": true, "spacing": "Small"},
		map[string]interface{}{"type": "TextBlock", "text": "💬 _" + witty + "_", "wrap": true, "spacing": "Medium"},
		map[string]interface{}{"type": "FactSet", "facts": factItems},
		map[string]interface{}{"type": "TextBlock", "text": fmt.Sprintf("📋 Team: **%s** | Sprint: **%s**", teamName, sprintName), "size": "Small", "isSubtle": true, "separator": true, "spacing": "Medium", "wrap": true},
		map[string]interface{}{"type": "TextBlock", "text": actionText, "weight": "Bolder", "size": "Medium", "color": alertColor, "spacing": "Medium", "wrap": true},
	}

	// Build content with msteams entities
	content := map[string]interface{}{
		"type":    "AdaptiveCard",
		"version": "1.5",
		"body":    bodyItems,
	}
	if mentionName != "" && mentionEmail != "" {
		content["msteams"] = map[string]interface{}{
			"entities": []interface{}{
				map[string]interface{}{
					"type": "mention",
					"text": "<at>" + mentionName + "</at>",
					"mentioned": map[string]string{
						"id":   mentionEmail,
						"name": mentionName,
					},
				},
			},
		}
	}

	card := map[string]interface{}{
		"type": "message",
		"attachments": []interface{}{map[string]interface{}{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content":     content,
		}},
	}

	body, _ := json.Marshal(card)
	resp, err := httpClient.Post(teamsWebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("  [Teams] Error: %s", err)
		return
	}
	resp.Body.Close()
	log.Printf("  [Teams] Sent: %s (%d) — %s", title, resp.StatusCode, witty)
}

func handleAlert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string              `json:"title"`
		Facts []map[string]string `json:"facts"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	sendTeamsNotification(req.Title, req.Facts)
	writeJSON(w, map[string]string{"status": "sent"})
}

func handleAlertAll(w http.ResponseWriter, r *http.Request) {
	proj := queryOr(r, "project", projectKey)
	team := queryOr(r, "team", teamName)
	sprintN := queryOr(r, "sprint", sprintName)
	boardID, _ := findBoard(proj)
	sprint, _ := findSprint(boardID, sprintN)
	if sprint == nil {
		writeJSON(w, map[string]string{"status": "failed"})
		return
	}
	issues, _ := getSprintIssues(sprint.ID, proj, team)
	tp := computeTimeProgress(sprint.StartDate, sprint.EndDate)
	summary := computeSummary(issues, tp)
	advanced := computeAdvanced(issues, tp, sprint.StartDate, sprint.EndDate)
	count := 0
	for _, e := range summary.PerEpic {
		if e.RiskStatus == "Not Deliverable" || e.RiskStatus == "At Risk" {
			sendTeamsNotification("Epic "+e.RiskStatus+": "+e.EpicKey, []map[string]string{{"name": "Epic", "value": e.EpicName}, {"name": "Progress", "value": fmt.Sprintf("%.0f%%", e.CompletionPct)}, {"name": "Owner", "value": e.EpicOwner}}, mention{Name: e.EpicOwner, Email: e.EpicOwnerEmail})
			count++
		}
	}
	for _, a := range advanced.AgingWIP {
		if a.DaysStuck >= 5 {
			sendTeamsNotification(fmt.Sprintf("Item Stuck %d Days", a.DaysStuck), []map[string]string{{"name": "Issue", "value": a.Key}, {"name": "Days", "value": fmt.Sprintf("%d", a.DaysStuck)}, {"name": "Assignee", "value": a.Assignee}}, mention{Name: a.Assignee, Email: a.AssigneeEmail})
			count++
		}
	}
	if advanced.Forecast.ForecastDelta < -2 {
		sendTeamsNotification("Sprint Behind Schedule", []map[string]string{{"name": "Forecast", "value": advanced.Forecast.Message}})
		count++
	}
	writeJSON(w, map[string]interface{}{"status": "alerts triggered", "alertCount": count})
}

func handleShareReport(w http.ResponseWriter, r *http.Request) {
	webhook := teamsGroupWebhook
	if webhook == "" {
		webhook = teamsWebhookURL
	}
	if webhook == "" {
		writeJSON(w, map[string]string{"status": "failed", "error": "No webhook"})
		return
	}
	proj := queryOr(r, "project", projectKey)
	team := queryOr(r, "team", teamName)
	sprintN := queryOr(r, "sprint", sprintName)
	boardID, _ := findBoard(proj)
	sprint, _ := findSprint(boardID, sprintN)
	if sprint == nil {
		writeJSON(w, map[string]string{"status": "failed"})
		return
	}
	issues, _ := getSprintIssues(sprint.ID, proj, team)
	tp := computeTimeProgress(sprint.StartDate, sprint.EndDate)
	summary := computeSummary(issues, tp)
	health := assessSprintHealth(summary, tp)
	advanced := computeAdvanced(issues, tp, sprint.StartDate, sprint.EndDate)
	forecastText := fmt.Sprintf("%dd late ⚠️", -advanced.Forecast.ForecastDelta)
	if advanced.Forecast.ForecastDelta >= 0 {
		forecastText = fmt.Sprintf("%dd early ✅", advanced.Forecast.ForecastDelta)
	}
	card := map[string]interface{}{
		"type": "message",
		"attachments": []interface{}{map[string]interface{}{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content": map[string]interface{}{
				"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
				"type": "AdaptiveCard", "version": "1.5",
				"body": []interface{}{
					map[string]interface{}{"type": "TextBlock", "text": "📊 SPRINT STATUS REPORT", "weight": "Bolder", "size": "Large"},
					map[string]interface{}{"type": "TextBlock", "text": fmt.Sprintf("**%s** • %s • %s", team, sprint.Name, time.Now().Format("Mon, Jan 2"))},
					map[string]interface{}{"type": "FactSet", "facts": []interface{}{
						map[string]string{"title": "🎯 Completion", "value": fmt.Sprintf("%.1f%%", summary.CompletionPct)},
						map[string]string{"title": "💚 Health", "value": health.Status},
						map[string]string{"title": "📈 Forecast", "value": forecastText},
						map[string]string{"title": "📦 Total", "value": fmt.Sprintf("%.0f pts", summary.TotalPoints)},
						map[string]string{"title": "✅ Done", "value": fmt.Sprintf("%.0f pts", summary.CompletedPoints)},
						map[string]string{"title": "🚀 Velocity", "value": fmt.Sprintf("%.1f pts/day", advanced.Forecast.DailyVelocity)},
						map[string]string{"title": "📐 Scope Creep", "value": fmt.Sprintf("%d%%", advanced.ScopeCreep.PctOfSprint)},
						map[string]string{"title": "⏰ Days Left", "value": fmt.Sprintf("%d working", tp.WorkingDaysRemain)},
					}},
					map[string]interface{}{"type": "TextBlock", "text": fmt.Sprintf("_🤖 Sprint Dashboard • %s_", time.Now().Format("3:04 PM")), "size": "Small", "isSubtle": true},
				},
			},
		}},
	}
	body, _ := json.Marshal(card)
	resp, err := httpClient.Post(webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, map[string]string{"status": "error"})
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		writeJSON(w, map[string]string{"status": "failed"})
		return
	}
	writeJSON(w, map[string]string{"status": "shared"})
}

func queryOr(r *http.Request, key, fallback string) string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	return v
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(data)
}

// ---------- Main ----------

func main() {
	if username == "" || token == "" {
		log.Fatal("Error: Set CONFLUENCE_USERNAME and CONFLUENCE_TOKEN environment variables.")
	}
	authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+token))

	httpClient = &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{},
			MaxIdleConns:      10,
			IdleConnTimeout:   90 * time.Second,
			DisableKeepAlives: false,
		},
	}

	// Load dashboard HTML — search multiple paths
	htmlPaths := []string{
		filepath.Join(filepath.Dir(os.Args[0]), "dashboard.html"),
		filepath.Join(".", "dashboard.html"),
		filepath.Join("..", "scripts", "dashboard.html"),
		filepath.Join(".", "scripts", "dashboard.html"),
	}
	for _, p := range htmlPaths {
		if data, err := os.ReadFile(p); err == nil {
			dashboardHTML = string(data)
			log.Printf("  HTML:    loaded from %s", p)
			break
		}
	}
	if dashboardHTML == "" {
		log.Fatal("ERROR: dashboard.html not found. Place it next to the binary or in ../scripts/")
	}

	fmt.Println("Sprint Status Dashboard (Go)")
	fmt.Printf("  Project: %s\n", projectKey)
	fmt.Printf("  Sprint:  %s\n", sprintName)
	fmt.Printf("  Team:    %s\n", teamName)
	fmt.Printf("  JIRA:    %s\n", jiraBaseURL)
	creds, credErr := getAWSCreds()
	if credErr == nil && creds.AccessKey != "" {
		fmt.Printf("  AI:      Bedrock Claude Sonnet 4 (%s)\n", awsRegion)
	} else if os.Getenv("GITHUB_TOKEN") != "" {
		fmt.Println("  AI:      GitHub Models (GPT-4o-mini, fallback)")
	} else {
		fmt.Println("  AI:      Disabled (no Bedrock creds or GITHUB_TOKEN)")
	}
	fmt.Printf("  Port:    %s\n", port)
	fmt.Printf("  Serving: http://localhost:%s\n", port)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}

		switch r.URL.Path {
		case "/api/defaults":
			handleDefaults(w, r)
		case "/api/all":
			handleAll(w, r)
		case "/api/sprints":
			handleSprints(w, r)
		case "/api/teams":
			handleTeams(w, r)
		case "/api/history":
			proj := queryOr(r, "project", projectKey)
			team := queryOr(r, "team", teamName)
			writeJSON(w, map[string]interface{}{"history": getHistory(proj, team)})
		case "/api/refresh":
			handleRefresh(w, r)
		case "/api/alert":
			handleAlert(w, r)
		case "/api/alert-all":
			handleAlertAll(w, r)
		case "/api/share-report":
			handleShareReport(w, r)
		default:
			handleRoot(w, r)
		}
	})

	log.Fatal(http.ListenAndServe(":"+port, handler))
}
