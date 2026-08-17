package plugin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// attributionPayload builds a webhook body with the full set of attribution fields, so
// tests can exercise acknowledgement, resolution and comment attribution independently.
type attributionPayload struct {
	incidentID     string
	name           string
	cause          string
	startedAt      string
	acknowledgedAt string
	acknowledgedBy string
	resolvedAt     string
	resolvedBy     string
	commentContent string
	commentAuthor  string
}

func (a attributionPayload) build() []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"text": "test",
		"data": map[string]interface{}{
			"id":   a.incidentID,
			"type": "incident",
			"attributes": map[string]interface{}{
				"name":                a.name,
				"url":                 "https://uptime.betterstack.com/incidents/" + a.incidentID,
				"cause":               a.cause,
				"started_at":          a.startedAt,
				"acknowledged_at":     a.acknowledgedAt,
				"acknowledged_by":     a.acknowledgedBy,
				"resolved_at":         a.resolvedAt,
				"resolved_by":         a.resolvedBy,
				"comment_content":     a.commentContent,
				"comment_author_name": a.commentAuthor,
			},
		},
	})
	return b
}

// incidentThread wires up a plugin whose incident post already exists, capturing the
// updated root post and every thread reply the webhook produces.
type incidentThread struct {
	plugin  *BetterStackPlugin
	api     *plugintest.API
	root    *model.Post
	replies []string
}

func newIncidentThread(rootMessage string) *incidentThread {
	th := &incidentThread{
		api:  &plugintest.API{},
		root: &model.Post{Id: "post-9", ChannelId: "test-channel", Message: rootMessage},
	}

	th.api.On("GetPost", "post-9").Return(th.root, nil)
	th.api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		th.replies = append(th.replies, post.Message)
		return true
	})).Return(&model.Post{Id: "reply-1"}, nil)
	th.api.On("UpdatePost", mock.Anything).Return(th.root, nil)
	th.api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	th.plugin = &BetterStackPlugin{}
	th.plugin.SetAPI(th.api)
	th.plugin.configuration = &configuration{AlertChannelID: "test-channel", WebhookToken: testWebhookToken}
	th.plugin.botUserID = "bot-user-id"
	th.plugin.kvstore = &mockKVStore{data: map[string]string{"inc-9": "post-9"}}

	return th
}

// send delivers a webhook and returns the re-rendered root post message.
func (th *incidentThread) send(t *testing.T, body []byte) string {
	t.Helper()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, webhookURL(testWebhookToken), bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	th.plugin.ServeHTTP(nil, w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	return th.root.Message
}

// TestIncidentReAcknowledgedByColleague covers the reported bug: an incident acknowledged
// by one person, reopened, then acknowledged by someone else must not keep attributing
// the acknowledgement to the first person.
func TestIncidentReAcknowledgedByColleague(t *testing.T) {
	assert := assert.New(t)

	first := attributionPayload{
		incidentID:     "inc-9",
		name:           "API down",
		cause:          "Status 500",
		startedAt:      "2024-01-01T10:00:00Z",
		acknowledgedAt: "2024-01-01T10:01:00Z",
		acknowledgedBy: "Johan Lejdung",
	}
	var firstPayload WebhookPayload
	assert.NoError(json.Unmarshal(first.build(), &firstPayload))

	th := newIncidentThread(firstPayload.FormatOriginalPost(statusAcknowledged))
	assert.Contains(th.root.Message, "by Johan Lejdung")

	// The incident is reopened and a colleague acknowledges it.
	message := th.send(t, attributionPayload{
		incidentID:     "inc-9",
		name:           "API down",
		cause:          "Status 500",
		startedAt:      "2024-01-01T10:00:00Z",
		acknowledgedAt: "2024-01-01T11:30:00Z",
		acknowledgedBy: "Anna Colleague",
	}.build())

	assert.Contains(message, "**Acknowledged:** **11:30:00 UTC** (Mon 01 Jan) by Anna Colleague")
	assert.NotContains(message, "Johan Lejdung")
	assert.Contains(th.replies[0], "**Acknowledged** by Anna Colleague")
}

// TestIncidentResolvedByAttribution verifies the resolver is credited with the
// resolution, rather than whoever acknowledged it earlier.
func TestIncidentResolvedByAttribution(t *testing.T) {
	assert := assert.New(t)

	th := newIncidentThread("## Incident: [API down](#)\n\n---\n**Latest status:** :bell: ACKNOWLEDGED")

	message := th.send(t, attributionPayload{
		incidentID:     "inc-9",
		name:           "API down",
		cause:          "Status 500",
		startedAt:      "2024-01-01T10:00:00Z",
		acknowledgedAt: "2024-01-01T10:01:00Z",
		acknowledgedBy: "Johan Lejdung",
		resolvedAt:     "2024-01-01T10:40:00Z",
		resolvedBy:     "Anna Colleague",
	}.build())

	assert.Contains(th.replies[0], "**Resolved** by Anna Colleague")
	assert.NotContains(th.replies[0], "Johan Lejdung")

	// Both attributions stand side by side on the root post.
	assert.Contains(message, "**Acknowledged:** **10:01:00 UTC** (Mon 01 Jan) by Johan Lejdung")
	assert.Contains(message, "**Resolved:** **10:40:00 UTC** (Mon 01 Jan) by Anna Colleague")
	assert.Contains(message, "**Latest status:** :white_check_mark: RESOLVED")
}

// TestIncidentReopened verifies an alarm on a resolved incident is reported as a reopen
// and that the stale resolution is cleared from the post.
func TestIncidentReopened(t *testing.T) {
	assert := assert.New(t)

	resolved := attributionPayload{
		incidentID:     "inc-9",
		name:           "API down",
		cause:          "Status 500",
		startedAt:      "2024-01-01T10:00:00Z",
		acknowledgedAt: "2024-01-01T10:01:00Z",
		acknowledgedBy: "Johan Lejdung",
		resolvedAt:     "2024-01-01T10:40:00Z",
		resolvedBy:     "Anna Colleague",
	}
	var resolvedPayload WebhookPayload
	assert.NoError(json.Unmarshal(resolved.build(), &resolvedPayload))

	th := newIncidentThread(resolvedPayload.FormatOriginalPost(statusResolved))

	// Better Stack clears the acknowledgement and resolution when reopening.
	message := th.send(t, attributionPayload{
		incidentID: "inc-9",
		name:       "API down",
		cause:      "Status 500 again",
		startedAt:  "2024-01-01T12:00:00Z",
	}.build())

	assert.Contains(th.replies[0], ":arrows_counterclockwise: **Reopened** — Status 500 again")
	assert.Contains(message, "**Latest status:** :arrows_counterclockwise: REOPENED")
	assert.Contains(message, "**Acknowledged:** _Not acknowledged_")
	assert.Contains(message, "**Resolved:** _Not resolved_")
	assert.NotContains(message, "Anna Colleague")
}

// TestIncidentAlarmIsNotAReopen guards the reopen heuristic: an alarm on a post that
// never showed RESOLVED is still an alarm.
func TestIncidentAlarmIsNotAReopen(t *testing.T) {
	assert := assert.New(t)

	th := newIncidentThread("## Incident: [API down](#)\n\n---\n**Latest status:** :bell: ACKNOWLEDGED")

	message := th.send(t, attributionPayload{
		incidentID: "inc-9",
		name:       "API down",
		cause:      "Status 500",
		startedAt:  "2024-01-01T10:00:00Z",
	}.build())

	assert.Contains(th.replies[0], ":red_circle: **ALARM**")
	assert.Contains(message, "**Latest status:** :red_circle: ALARM")
}

// TestIncidentComment verifies comment events are threaded and do not restate the status.
func TestIncidentComment(t *testing.T) {
	assert := assert.New(t)

	th := newIncidentThread("## Incident: [API down](#)\n\n---\n**Latest status:** :bell: ACKNOWLEDGED")

	th.send(t, attributionPayload{
		incidentID:     "inc-9",
		name:           "API down",
		cause:          "Status 500",
		startedAt:      "2024-01-01T10:00:00Z",
		acknowledgedAt: "2024-01-01T10:01:00Z",
		acknowledgedBy: "Johan Lejdung",
		commentContent: "Rolling back the deploy.\nWill confirm shortly.",
		commentAuthor:  "Anna Colleague",
	}.build())

	assert.Len(th.replies, 1)
	assert.Equal(":speech_balloon: **Comment** from Anna Colleague:\n\n> Rolling back the deploy.\n> Will confirm shortly.", th.replies[0])
}

// TestUnexpandedPlaceholdersAreIgnored verifies that template variables Better Stack
// could not fill are not rendered into posts as literal text.
func TestUnexpandedPlaceholdersAreIgnored(t *testing.T) {
	assert := assert.New(t)

	th := newIncidentThread("## Incident: [API down](#)\n\n---\n**Latest status:** :bell: ACKNOWLEDGED")

	message := th.send(t, attributionPayload{
		incidentID:     "inc-9",
		name:           "API down",
		cause:          "Status 500",
		startedAt:      "2024-01-01T10:00:00Z",
		acknowledgedAt: "2024-01-01T10:01:00Z",
		acknowledgedBy: "$ACKNOWLEDGED_BY",
		resolvedAt:     "$RESOLVED_AT",
		resolvedBy:     "$RESOLVED_BY",
		commentContent: "$COMMENT_CONTENT",
		commentAuthor:  "$COMMENT_AUTHOR_NAME",
	}.build())

	assert.NotContains(message, "$")
	assert.Contains(message, "**Acknowledged:** **10:01:00 UTC** (Mon 01 Jan)")
	assert.Contains(message, "**Resolved:** _Not resolved_")
	// The unexpanded comment must not be mistaken for a real comment event.
	assert.Contains(th.replies[0], "**Acknowledged**")
}

// TestPreviousStatusFromPost covers footer parsing, including posts written before the
// emoji labels were introduced.
func TestPreviousStatusFromPost(t *testing.T) {
	assert := assert.New(t)

	cases := map[string]string{
		"## Incident\n\n---\n**Latest status:** :white_check_mark: RESOLVED":         statusResolved,
		"## Incident\n\n---\n**Latest status:** :bell: ACKNOWLEDGED":                 statusAcknowledged,
		"## Incident\n\n---\n**Latest status:** :arrows_counterclockwise: REOPENED":  statusReopened,
		"## Incident\n\n---\n**Latest status:** ALARM":                               statusAlarm,
		"## Incident with no footer at all":                                          "",
		"RESOLVED appears in the body\n\n---\n**Latest status:** :red_circle: ALARM": statusAlarm,
	}

	for message, expected := range cases {
		assert.Equal(expected, previousStatusFromPost(message), message)
	}
}
