package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const betterStackBaseURL = "https://uptime.betterstack.com/api"

// formatTime parses an ISO 8601 timestamp (as sent by Better Stack) and returns a
// human-readable string like "Mon 09 Mar 2026 10:30:00 UTC". Returns the original
// string unchanged if it cannot be parsed.
func formatTime(ts string) string {
	if ts == "" {
		return ts
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try without timezone suffix (some Better Stack fields omit it)
		t, err = time.Parse("2006-01-02T15:04:05", ts)
		if err != nil {
			return ts
		}
	}
	return t.UTC().Format("**15:04:05 UTC** (Mon 02 Jan)")
}

// BetterStackClient is a minimal HTTP client for the Better Stack Uptime API.
type BetterStackClient struct {
	token      string
	httpClient *http.Client
}

// newBetterStackClient creates a new API client authenticated with the given token.
func newBetterStackClient(token string) *BetterStackClient {
	return &BetterStackClient{
		token: token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *BetterStackClient) get(path string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, betterStackBaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Better Stack API returned %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// On-call schedules
// ---------------------------------------------------------------------------

// OnCallUser represents a user currently on call.
type OnCallUser struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Meta struct {
		Email string `json:"email"`
	} `json:"meta"`
}

// OnCallUserDetail contains the full user attributes returned in the `included` array.
type OnCallUserDetail struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		FirstName    string   `json:"first_name"`
		LastName     string   `json:"last_name"`
		Email        string   `json:"email"`
		PhoneNumbers []string `json:"phone_numbers"`
	} `json:"attributes"`
}

// OnCallSchedule represents a single on-call calendar.
type OnCallSchedule struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name            string `json:"name"`
		DefaultCalendar bool   `json:"default_calendar"`
		TeamName        string `json:"team_name"`
	} `json:"attributes"`
	Relationships struct {
		OnCallUsers struct {
			Data []OnCallUser `json:"data"`
		} `json:"on_call_users"`
	} `json:"relationships"`
}

type onCallResponse struct {
	Data     []OnCallSchedule   `json:"data"`
	Included []OnCallUserDetail `json:"included"`
}

// OnCallEvent represents a single scheduled on-call event (shift) for a calendar.
type OnCallEvent struct {
	ID       int      `json:"id"`
	Users    []string `json:"users"` // list of email addresses
	StartsAt string   `json:"starts_at"`
	EndsAt   string   `json:"ends_at"`
	Override bool     `json:"override"`
}

type onCallEventsResponse struct {
	Events []OnCallEvent `json:"events"`
}

// GetOnCallEvents returns all scheduled events for the given on-call schedule.
// Events are in chronological order and include both past and future shifts.
func (c *BetterStackClient) GetOnCallEvents(scheduleID string) ([]OnCallEvent, error) {
	var resp onCallEventsResponse
	if err := c.get("/v2/on-calls/"+scheduleID+"/events", &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}

// GetOnCallSchedules returns all on-call schedules with their currently on-call users.
func (c *BetterStackClient) GetOnCallSchedules() ([]OnCallSchedule, map[string]OnCallUserDetail, error) {
	var resp onCallResponse
	if err := c.get("/v2/on-calls", &resp); err != nil {
		return nil, nil, err
	}

	// Build a lookup map from user ID → user details for easy joining.
	userDetails := make(map[string]OnCallUserDetail, len(resp.Included))
	for _, u := range resp.Included {
		userDetails[u.ID] = u
	}

	return resp.Data, userDetails, nil
}

// ---------------------------------------------------------------------------
// Incidents
// ---------------------------------------------------------------------------

// Incident represents a single Better Stack incident.
type Incident struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name           string  `json:"name"`
		URL            string  `json:"url"`
		HTTPMethod     string  `json:"http_method"`
		Cause          string  `json:"cause"`
		StartedAt      string  `json:"started_at"`
		AcknowledgedAt *string `json:"acknowledged_at"`
		AcknowledgedBy *string `json:"acknowledged_by"`
		ResolvedAt     *string `json:"resolved_at"`
		ResolvedBy     *string `json:"resolved_by"`
		Status         string  `json:"status"`
		TeamName       string  `json:"team_name"`
	} `json:"attributes"`
}

type incidentsResponse struct {
	Data []Incident `json:"data"`
}

// GetActiveIncidents returns all currently unresolved incidents.
func (c *BetterStackClient) GetActiveIncidents() ([]Incident, error) {
	var resp incidentsResponse
	if err := c.get("/v3/incidents?resolved=false", &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ---------------------------------------------------------------------------
// Webhook payload types
// ---------------------------------------------------------------------------

// WebhookPayload is the body sent by Better Stack on incident status changes.
type WebhookPayload struct {
	Text     string          `json:"text"`
	Priority json.RawMessage `json:"priority"`
	Data     struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Name            string `json:"name"`
			URL             string `json:"url"`
			HTTPMethod      string `json:"http_method"`
			Cause           string `json:"cause"`
			StartedAt       string `json:"started_at"`
			AcknowledgedAt  string `json:"acknowledged_at"`
			AcknowledgedBy  string `json:"acknowledged_by"`
			ResolvedAt      string `json:"resolved_at"`
			ResponseContent string `json:"response_content"`
			ResponseURL     string `json:"response_url"`
			ScreenshotURL   string `json:"screenshot_url"`
			SurroundingLogs string `json:"surrounding_logs"`
		} `json:"attributes"`
	} `json:"data"`
}

// Status returns a human-readable status string derived from the incident timestamps.
func (p *WebhookPayload) Status() string {
	attrs := p.Data.Attributes
	switch {
	case attrs.ResolvedAt != "":
		return "RESOLVED"
	case attrs.AcknowledgedAt != "":
		return "ACKNOWLEDGED"
	default:
		return "ALARM"
	}
}

// StatusLabel returns the emoji + status text used in both the footer and thread replies.
func (p *WebhookPayload) StatusLabel() string {
	switch p.Status() {
	case "RESOLVED":
		return ":white_check_mark: RESOLVED"
	case "ACKNOWLEDGED":
		return ":bell: ACKNOWLEDGED"
	default:
		return ":red_circle: ALARM"
	}
}

// FormatOriginalPost builds the full markdown message for the original incident post.
// The footer line contains the latest status and will be updated on each webhook call.
func (p *WebhookPayload) FormatOriginalPost() string {
	attrs := p.Data.Attributes

	ackLine := "_Not acknowledged_"
	if attrs.AcknowledgedAt != "" {
		ackLine = formatTime(attrs.AcknowledgedAt)
		if attrs.AcknowledgedBy != "" {
			ackLine += " by " + attrs.AcknowledgedBy
		}
	}

	resolvedLine := "_Not resolved_"
	if attrs.ResolvedAt != "" {
		resolvedLine = formatTime(attrs.ResolvedAt)
	}

	incidentURL := attrs.URL
	if incidentURL == "" {
		incidentURL = "#"
	}

	body := fmt.Sprintf("## Incident: [%s](%s)\n\n"+
		"**Cause:** %s\n\n"+
		"**Started:** %s\n"+
		"**Acknowledged:** %s\n"+
		"**Resolved:** %s\n\n"+
		"---\n\n"+
		"[View Full Details](%s)\n\n"+
		"---\n"+
		"**Latest status:** %s",
		attrs.Name, incidentURL,
		attrs.Cause,
		formatTime(attrs.StartedAt),
		ackLine,
		resolvedLine,
		incidentURL,
		p.StatusLabel(),
	)

	return body
}

// FormatLogsReply builds a thread reply containing the surrounding log lines.
// Returns an empty string if there are no surrounding logs or if the value is
// the literal unexpanded Better Stack template placeholder.
func (p *WebhookPayload) FormatLogsReply() string {
	logs := p.Data.Attributes.SurroundingLogs
	if logs == "" || logs == "$METADATA.Surrounding logs" {
		return ""
	}
	return "**Surrounding logs:**\n```\n" + logs + "\n```"
}

// FormatThreadReply builds a short status-update message for posting in the incident thread.
func (p *WebhookPayload) FormatThreadReply() string {
	attrs := p.Data.Attributes
	status := p.Status()

	switch status {
	case "RESOLVED":
		by := ""
		if attrs.AcknowledgedBy != "" {
			by = " by " + attrs.AcknowledgedBy
		}
		return fmt.Sprintf(":white_check_mark: **Resolved**%s at %s", by, formatTime(attrs.ResolvedAt))
	case "ACKNOWLEDGED":
		by := ""
		if attrs.AcknowledgedBy != "" {
			by = " by " + attrs.AcknowledgedBy
		}
		return fmt.Sprintf(":bell: **Acknowledged**%s at %s", by, formatTime(attrs.AcknowledgedAt))
	default:
		return fmt.Sprintf(":red_circle: **ALARM** — %s (started %s)", attrs.Cause, formatTime(attrs.StartedAt))
	}
}

// updateStatusFooter replaces the `**Latest status:** …` line at the end of an existing post
// with the new status from this payload. If the footer line is not found, it is appended.
func (p *WebhookPayload) updateStatusFooter(existing string) string {
	newFooter := "\n---\n**Latest status:** " + p.StatusLabel()

	// Find the last occurrence of the separator before the status footer.
	const marker = "\n---\n**Latest status:**"
	if idx := strings.LastIndex(existing, marker); idx >= 0 {
		return existing[:idx] + newFooter
	}
	// Footer not found — append it.
	return existing + newFooter
}

// ---------------------------------------------------------------------------
// Monitors
// ---------------------------------------------------------------------------

// Monitor represents a single Better Stack uptime monitor.
type Monitor struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		URL              string  `json:"url"`
		PronouncableName string  `json:"pronounceable_name"`
		MonitorType      string  `json:"monitor_type"`
		Status           string  `json:"status"`
		TeamName         string  `json:"team_name"`
		CheckFrequency   int     `json:"check_frequency"`
		LastCheckedAt    string  `json:"last_checked_at"`
		PausedAt         *string `json:"paused_at"`
	} `json:"attributes"`
}

type monitorsResponse struct {
	Data []Monitor `json:"data"`
}

// GetMonitors returns all monitors, optionally filtered to only those with a given status.
// Pass an empty string for status to return all monitors.
func (c *BetterStackClient) GetMonitors(statusFilter string) ([]Monitor, error) {
	var resp monitorsResponse
	if err := c.get("/v2/monitors", &resp); err != nil {
		return nil, err
	}
	if statusFilter == "" {
		return resp.Data, nil
	}
	filtered := resp.Data[:0]
	for _, m := range resp.Data {
		if m.Attributes.Status == statusFilter {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// ---------------------------------------------------------------------------
// Status pages
// ---------------------------------------------------------------------------

// StatusPage represents a single Better Stack status page.
type StatusPage struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		CompanyName    string `json:"company_name"`
		Subdomain      string `json:"subdomain"`
		CustomDomain   string `json:"custom_domain"`
		AggregateState string `json:"aggregate_state"`
		Timezone       string `json:"timezone"`
	} `json:"attributes"`
}

type statusPagesResponse struct {
	Data []StatusPage `json:"data"`
}

// GetStatusPages returns all status pages.
func (c *BetterStackClient) GetStatusPages() ([]StatusPage, error) {
	var resp statusPagesResponse
	if err := c.get("/v2/status-pages", &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ---------------------------------------------------------------------------

// lastIndex returns the last index of substr in s, or -1 if not present.
func lastIndex(s, substr string) int {
	last := -1
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			last = i
		}
	}
	return last
}
