package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
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
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05 UTC", // Better Stack test webhook format
	}
	for _, f := range formats {
		if t, err := time.Parse(f, ts); err == nil {
			return t.UTC().Format("**15:04:05 UTC** (Mon 02 Jan)")
		}
	}
	return ts
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

// Ping performs the cheapest authenticated call available against the Better Stack
// Uptime API to verify that the API is reachable and the configured token is valid.
// It requests a single monitor and discards the payload.
func (c *BetterStackClient) Ping() error {
	var resp struct{}
	return c.get("/v2/monitors?per_page=1", &resp)
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
			ResolvedBy      string `json:"resolved_by"`
			ResponseContent string `json:"response_content"`
			ResponseURL     string `json:"response_url"`
			ScreenshotURL   string `json:"screenshot_url"`
			SurroundingLogs string `json:"surrounding_logs"`

			// Comment fields are only populated on incident comment events.
			CommentID          string `json:"comment_id"`
			CommentContent     string `json:"comment_content"`
			CommentCreatedAt   string `json:"comment_created_at"`
			CommentAuthorName  string `json:"comment_author_name"`
			CommentAuthorEmail string `json:"comment_author_email"`
		} `json:"attributes"`
	} `json:"data"`
}

// placeholderFields pairs each payload field with the Better Stack template variable
// that fills it. Better Stack leaves a variable unexpanded when the incident carries no
// value for it — an alarm webhook arrives with the literal text "$RESOLVED_BY" rather
// than an empty string — so the literal must be treated as absent before rendering.
//
// The comparison is exact, so a real value that merely starts with "$" is left alone.
func (p *WebhookPayload) placeholderFields() map[*string][]string {
	attrs := &p.Data.Attributes
	return map[*string][]string{
		&attrs.Name:               {"$NAME"},
		&attrs.URL:                {"$INCIDENT_URL", "$URL"},
		&attrs.HTTPMethod:         {"$HTTP_METHOD"},
		&attrs.Cause:              {"$CAUSE"},
		&attrs.StartedAt:          {"$STARTED_AT", "$STARTED_AT_ISO8601"},
		&attrs.AcknowledgedAt:     {"$ACKNOWLEDGED_AT", "$ACKNOWLEDGED_AT_ISO8601"},
		&attrs.AcknowledgedBy:     {"$ACKNOWLEDGED_BY"},
		&attrs.ResolvedAt:         {"$RESOLVED_AT", "$RESOLVED_AT_ISO8601"},
		&attrs.ResolvedBy:         {"$RESOLVED_BY"},
		&attrs.ResponseContent:    {"$RESPONSE_CONTENT"},
		&attrs.ResponseURL:        {"$RESPONSE_URL"},
		&attrs.ScreenshotURL:      {"$SCREENSHOT_URL"},
		&attrs.SurroundingLogs:    {"$METADATA.Surrounding logs", "$METADATA_ARRAY"},
		&attrs.CommentID:          {"$COMMENT_ID"},
		&attrs.CommentContent:     {"$COMMENT_CONTENT"},
		&attrs.CommentCreatedAt:   {"$COMMENT_CREATED_AT", "$COMMENT_CREATED_AT_ISO8601"},
		&attrs.CommentAuthorName:  {"$COMMENT_AUTHOR_NAME"},
		&attrs.CommentAuthorEmail: {"$COMMENT_AUTHOR_EMAIL"},
	}
}

// normalize blanks every field that still holds its unexpanded template variable, so the
// rest of the plugin can treat "absent" as an empty string.
func (p *WebhookPayload) normalize() {
	for field, placeholders := range p.placeholderFields() {
		if slices.Contains(placeholders, *field) {
			*field = ""
		}
	}
}

// IsComment reports whether this webhook is an incident comment event rather than a
// status change.
func (p *WebhookPayload) IsComment() bool {
	return p.Data.Attributes.CommentContent != ""
}

// Incident statuses shown in the post footer and thread replies.
const (
	statusAlarm        = "ALARM"
	statusAcknowledged = "ACKNOWLEDGED"
	statusResolved     = "RESOLVED"
	statusReopened     = "REOPENED"
)

// Status returns a human-readable status string derived from the incident timestamps.
func (p *WebhookPayload) Status() string {
	attrs := p.Data.Attributes
	switch {
	case attrs.ResolvedAt != "":
		return statusResolved
	case attrs.AcknowledgedAt != "":
		return statusAcknowledged
	default:
		return statusAlarm
	}
}

// EffectiveStatus refines Status using the status the incident post showed before this
// webhook arrived. Better Stack clears the timestamps when an incident is reopened, which
// is indistinguishable from a fresh alarm on timestamps alone — but an incident that
// alarms again after being resolved has been reopened. Pass an empty previousStatus for
// an incident we have not posted about yet.
func (p *WebhookPayload) EffectiveStatus(previousStatus string) string {
	status := p.Status()
	if status == statusAlarm && previousStatus == statusResolved {
		return statusReopened
	}
	return status
}

// statusLabel returns the emoji + status text used in both the footer and thread replies.
func statusLabel(status string) string {
	switch status {
	case statusResolved:
		return ":white_check_mark: RESOLVED"
	case statusAcknowledged:
		return ":bell: ACKNOWLEDGED"
	case statusReopened:
		return ":arrows_counterclockwise: REOPENED"
	default:
		return ":red_circle: ALARM"
	}
}

// previousStatusFromPost reads the status recorded in an existing incident post's footer.
// Returns an empty string if the post has no recognisable footer.
func previousStatusFromPost(message string) string {
	idx := strings.LastIndex(message, "**Latest status:**")
	if idx < 0 {
		return ""
	}
	footer := message[idx:]
	for _, status := range []string{statusReopened, statusResolved, statusAcknowledged, statusAlarm} {
		if strings.Contains(footer, status) {
			return status
		}
	}
	return ""
}

// FormatOriginalPost builds the full markdown message for the incident post. It is
// re-rendered from the payload on every webhook for the incident, so the acknowledged and
// resolved lines always reflect the latest state — including a reopen, which clears them.
func (p *WebhookPayload) FormatOriginalPost(status string) string {
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
		if attrs.ResolvedBy != "" {
			resolvedLine += " by " + attrs.ResolvedBy
		}
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
		statusLabel(status),
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

// FormatThreadReply builds a short status-update message for posting in the incident
// thread. Each status is attributed to the person who performed that action: an
// acknowledgement to the acknowledger, a resolution to the resolver.
func (p *WebhookPayload) FormatThreadReply(status string) string {
	attrs := p.Data.Attributes

	switch status {
	case statusResolved:
		return fmt.Sprintf(":white_check_mark: **Resolved**%s at %s", by(attrs.ResolvedBy), formatTime(attrs.ResolvedAt))
	case statusAcknowledged:
		return fmt.Sprintf(":bell: **Acknowledged**%s at %s", by(attrs.AcknowledgedBy), formatTime(attrs.AcknowledgedAt))
	case statusReopened:
		return fmt.Sprintf(":arrows_counterclockwise: **Reopened** — %s", attrs.Cause)
	default:
		return fmt.Sprintf(":red_circle: **ALARM** — %s (started %s)", attrs.Cause, formatTime(attrs.StartedAt))
	}
}

// FormatCommentReply builds a thread reply for an incident comment event. Returns an
// empty string if the payload carries no comment.
func (p *WebhookPayload) FormatCommentReply() string {
	attrs := p.Data.Attributes
	if attrs.CommentContent == "" {
		return ""
	}

	author := attrs.CommentAuthorName
	if author == "" {
		author = attrs.CommentAuthorEmail
	}
	if author == "" {
		author = "someone"
	}

	return fmt.Sprintf(":speech_balloon: **Comment** from %s:\n\n> %s",
		author, strings.ReplaceAll(attrs.CommentContent, "\n", "\n> "))
}

// by renders an optional " by <name>" suffix, empty when the name is unknown.
func by(name string) string {
	if name == "" {
		return ""
	}
	return " by " + name
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
