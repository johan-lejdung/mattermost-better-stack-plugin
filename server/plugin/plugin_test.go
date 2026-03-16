package plugin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const testWebhookToken = "test-secret-token-abc123"

// buildWebhookPayload returns a minimal WebhookPayload JSON body for the given incident fields.
func buildWebhookPayload(incidentID, name, cause, startedAt, acknowledgedAt, resolvedAt string) []byte {
	p := map[string]interface{}{
		"text": "test",
		"data": map[string]interface{}{
			"id":   incidentID,
			"type": "incident",
			"attributes": map[string]interface{}{
				"name":            name,
				"url":             "https://uptime.betterstack.com/incidents/" + incidentID,
				"cause":           cause,
				"started_at":      startedAt,
				"acknowledged_at": acknowledgedAt,
				"resolved_at":     resolvedAt,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

func webhookURL(token string) string {
	return "/api/v1/webhook/" + token
}

func TestWebhookNewIncident(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}

	createdPost := &model.Post{Id: "post-123"}

	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.ChannelId == "test-channel"
	})).Return(createdPost, nil)
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	p := &BetterStackPlugin{}
	p.SetAPI(api)
	p.configuration = &configuration{
		BetterStackUptimeAPIToken: "test-token",
		AlertChannelID:            "test-channel",
		WebhookToken:              testWebhookToken,
	}
	p.botUserID = "bot-user-id"
	p.kvstore = &mockKVStore{data: map[string]string{}}

	body := buildWebhookPayload("inc-1", "Homepage down", "Status 404", "2024-01-01T10:00:00Z", "", "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, webhookURL(testWebhookToken), bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	p.ServeHTTP(nil, w, r)

	assert.Equal(http.StatusOK, w.Code)
	api.AssertCalled(t, "CreatePost", mock.Anything)
}

func TestWebhookExistingIncident(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}

	existingPost := &model.Post{
		Id:        "post-123",
		ChannelId: "test-channel",
		Message:   "## Incident: [Homepage down](https://example.com)\n\n---\n**Latest status:** ALARM",
	}

	api.On("GetPost", "post-123").Return(existingPost, nil)
	api.On("UpdatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.Id == "post-123"
	})).Return(existingPost, nil)
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "post-123"
	})).Return(&model.Post{Id: "reply-456"}, nil)
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	kv := &mockKVStore{data: map[string]string{
		"inc-1": "post-123",
	}}

	p := &BetterStackPlugin{}
	p.SetAPI(api)
	p.configuration = &configuration{
		BetterStackUptimeAPIToken: "test-token",
		AlertChannelID:            "test-channel",
		WebhookToken:              testWebhookToken,
	}
	p.botUserID = "bot-user-id"
	p.kvstore = kv

	body := buildWebhookPayload("inc-1", "Homepage down", "Status 404", "2024-01-01T10:00:00Z", "2024-01-01T10:05:00Z", "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, webhookURL(testWebhookToken), bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	p.ServeHTTP(nil, w, r)

	assert.Equal(http.StatusOK, w.Code)
	api.AssertCalled(t, "CreatePost", mock.Anything)
	api.AssertCalled(t, "UpdatePost", mock.Anything)
}

func TestWebhookWrongToken(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}

	p := &BetterStackPlugin{}
	p.SetAPI(api)
	p.configuration = &configuration{
		BetterStackUptimeAPIToken: "test-token",
		AlertChannelID:            "test-channel",
		WebhookToken:              testWebhookToken,
	}
	p.botUserID = "bot-user-id"
	p.kvstore = &mockKVStore{data: map[string]string{}}

	body := buildWebhookPayload("inc-1", "Test", "cause", "2024-01-01T10:00:00Z", "", "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, webhookURL("wrong-token"), bytes.NewReader(body))

	p.ServeHTTP(nil, w, r)

	assert.Equal(http.StatusUnauthorized, w.Code)
}

func TestWebhookBasicAuthRejected(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}

	p := &BetterStackPlugin{}
	p.SetAPI(api)
	p.configuration = &configuration{
		BetterStackUptimeAPIToken: "test-token",
		AlertChannelID:            "test-channel",
		WebhookToken:              testWebhookToken,
		WebhookUsername:           "user",
		WebhookPassword:           "pass",
	}
	p.botUserID = "bot-user-id"
	p.kvstore = &mockKVStore{data: map[string]string{}}

	body := buildWebhookPayload("inc-1", "Test", "cause", "2024-01-01T10:00:00Z", "", "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, webhookURL(testWebhookToken), bytes.NewReader(body))
	r.SetBasicAuth("wrong-user", "wrong-pass")

	p.ServeHTTP(nil, w, r)

	assert.Equal(http.StatusUnauthorized, w.Code)
}

func TestWebhookBasicAuthAccepted(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}

	createdPost := &model.Post{Id: "post-123"}
	api.On("CreatePost", mock.Anything).Return(createdPost, nil)
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	p := &BetterStackPlugin{}
	p.SetAPI(api)
	p.configuration = &configuration{
		BetterStackUptimeAPIToken: "test-token",
		AlertChannelID:            "test-channel",
		WebhookToken:              testWebhookToken,
		WebhookUsername:           "user",
		WebhookPassword:           "pass",
	}
	p.botUserID = "bot-user-id"
	p.kvstore = &mockKVStore{data: map[string]string{}}

	body := buildWebhookPayload("inc-1", "Test", "cause", "2024-01-01T10:00:00Z", "", "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, webhookURL(testWebhookToken), bytes.NewReader(body))
	r.SetBasicAuth("user", "pass")

	p.ServeHTTP(nil, w, r)

	assert.Equal(http.StatusOK, w.Code)
}

func TestFormatLogsReply(t *testing.T) {
	cases := []struct {
		name     string
		logs     string
		expected string
	}{
		{
			name:     "empty string returns empty",
			logs:     "",
			expected: "",
		},
		{
			name:     "unexpanded Better Stack placeholder returns empty",
			logs:     "$METADATA.Surrounding logs",
			expected: "",
		},
		{
			name:     "real log content is formatted",
			logs:     "2024-01-01 error: something failed",
			expected: "**Surrounding logs:**\n```\n2024-01-01 error: something failed\n```",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &WebhookPayload{}
			p.Data.Attributes.SurroundingLogs = tc.logs
			assert.Equal(t, tc.expected, p.FormatLogsReply())
		})
	}
}

// mockKVStore is an in-memory KVStore implementation for tests.
// Keys are the raw incidentID — the real kvstore package adds its own prefix.
type mockKVStore struct {
	data map[string]string
}

func (m *mockKVStore) StoreIncidentPost(incidentID string, postID string) error {
	m.data[incidentID] = postID
	return nil
}

func (m *mockKVStore) GetIncidentPost(incidentID string) (string, error) {
	return m.data[incidentID], nil
}

func (m *mockKVStore) StoreOnCallState(scheduleID string, userIDs []string) error {
	m.data["oncall_"+scheduleID] = strings.Join(userIDs, ",")
	return nil
}

func (m *mockKVStore) GetOnCallState(scheduleID string) ([]string, error) {
	v := m.data["oncall_"+scheduleID]
	if v == "" {
		return nil, nil
	}
	return strings.Split(v, ","), nil
}

func (m *mockKVStore) StoreLastDigestDate(date string) error {
	m.data["last_digest"] = date
	return nil
}

func (m *mockKVStore) GetLastDigestDate() (string, error) {
	return m.data["last_digest"], nil
}
