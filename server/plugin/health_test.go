package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func healthURL(token string) string {
	return "/api/v1/health/" + token
}

// stubPing replaces the Better Stack API probe for the duration of a test and records
// how many times it was called.
func stubPing(t *testing.T, err error) *int {
	t.Helper()

	calls := 0
	original := pingBetterStack
	pingBetterStack = func(string) error {
		calls++
		return err
	}
	t.Cleanup(func() { pingBetterStack = original })

	return &calls
}

// newHealthPlugin returns a plugin configured so that every health check passes.
func newHealthPlugin(api *plugintest.API) *BetterStackPlugin {
	p := &BetterStackPlugin{}
	p.SetAPI(api)
	p.configuration = &configuration{
		BetterStackUptimeAPIToken: "test-token",
		AlertChannelID:            "test-channel",
		WebhookToken:              testWebhookToken,
	}
	p.botUserID = "bot-user-id"
	return p
}

func doHealthRequest(p *BetterStackPlugin, token string) (*httptest.ResponseRecorder, healthResponse) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, healthURL(token), nil)
	p.ServeHTTP(nil, w, r)

	var body healthResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	return w, body
}

func TestHealthHealthy(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("GetChannel", "test-channel").Return(&model.Channel{Id: "test-channel"}, nil)

	stubPing(t, nil)

	w, body := doHealthRequest(newHealthPlugin(api), testWebhookToken)

	assert.Equal(http.StatusOK, w.Code)
	assert.Equal("application/json", w.Header().Get("Content-Type"))
	assert.Equal("no-store", w.Header().Get("Cache-Control"))
	assert.Equal(healthStatusOK, body.Status)
	assert.Len(body.Checks, 3)
	for _, check := range body.Checks {
		assert.Equal(healthStatusOK, check.Status, check.Name)
	}
}

func TestHealthWrongToken(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	w, _ := doHealthRequest(newHealthPlugin(api), "not-the-token")

	assert.Equal(http.StatusUnauthorized, w.Code)
	// Nothing about the plugin's state may leak to an unauthorized caller.
	api.AssertNotCalled(t, "GetChannel", mock.Anything)
}

func TestHealthTokenNotConfigured(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	p := newHealthPlugin(api)
	p.configuration.WebhookToken = ""

	w, _ := doHealthRequest(p, "anything")

	assert.Equal(http.StatusServiceUnavailable, w.Code)
}

// TestHealthIgnoresBasicAuth verifies the uptime check is authorized by its secret token
// alone. The webhook's optional Basic Auth credentials are for Better Stack's incident
// posts; requiring them here would mean handing them to every uptime monitor as well.
func TestHealthIgnoresBasicAuth(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("GetChannel", "test-channel").Return(&model.Channel{Id: "test-channel"}, nil)

	stubPing(t, nil)

	p := newHealthPlugin(api)
	p.configuration.WebhookUsername = "monitor"
	p.configuration.WebhookPassword = "hunter2"

	// No credentials — still authorized by the token in the path.
	w, _ := doHealthRequest(p, testWebhookToken)
	assert.Equal(http.StatusOK, w.Code)

	// Supplying credentials is harmless.
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, healthURL(testWebhookToken), nil)
	r.SetBasicAuth("monitor", "hunter2")
	p.ServeHTTP(nil, w, r)
	assert.Equal(http.StatusOK, w.Code)

	// The wrong token is still rejected regardless.
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	w, _ = doHealthRequest(p, "not-the-token")
	assert.Equal(http.StatusUnauthorized, w.Code)
}

// TestWebhookStillRequiresBasicAuth guards that decoupling the uptime check did not
// relax the webhook.
func TestWebhookStillRequiresBasicAuth(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	p := newHealthPlugin(api)
	p.configuration.WebhookUsername = "betterstack"
	p.configuration.WebhookPassword = "hunter2"
	p.kvstore = &mockKVStore{data: map[string]string{}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, webhookURL(testWebhookToken),
		bytes.NewReader(buildWebhookPayload("inc-1", "Down", "500", "2024-01-01T10:00:00Z", "", "")))
	p.ServeHTTP(nil, w, r)

	assert.Equal(http.StatusUnauthorized, w.Code)
	assert.Equal(`Basic realm="Better Stack plugin"`, w.Header().Get("WWW-Authenticate"))
	api.AssertNotCalled(t, "CreatePost", mock.Anything)
}

func TestHealthAlertChannelMissing(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("GetChannel", "test-channel").Return(nil, &model.AppError{Message: "channel not found"})
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	stubPing(t, nil)

	w, body := doHealthRequest(newHealthPlugin(api), testWebhookToken)

	assert.Equal(http.StatusServiceUnavailable, w.Code)
	assert.Equal(healthStatusFail, body.Status)
	assert.Equal("alert_channel", body.Checks[1].Name)
	assert.Equal(healthStatusFail, body.Checks[1].Status)
	assert.Contains(body.Checks[1].Detail, "channel not found")
}

func TestHealthAlertChannelNotConfigured(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	stubPing(t, nil)

	p := newHealthPlugin(api)
	p.configuration.AlertChannelID = ""

	w, body := doHealthRequest(p, testWebhookToken)

	assert.Equal(http.StatusServiceUnavailable, w.Code)
	assert.Equal("alert channel is not configured", body.Checks[1].Detail)
	api.AssertNotCalled(t, "GetChannel", mock.Anything)
}

func TestHealthBetterStackAPIUnreachable(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("GetChannel", "test-channel").Return(&model.Channel{Id: "test-channel"}, nil)
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	stubPing(t, errors.New("Better Stack API returned 401: unauthorized"))

	w, body := doHealthRequest(newHealthPlugin(api), testWebhookToken)

	assert.Equal(http.StatusServiceUnavailable, w.Code)
	assert.Equal(healthStatusFail, body.Status)
	assert.Equal("better_stack_api", body.Checks[2].Name)
	assert.Contains(body.Checks[2].Detail, "401")
}

func TestHealthBotAccountMissing(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("GetChannel", "test-channel").Return(&model.Channel{Id: "test-channel"}, nil)
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	stubPing(t, nil)

	p := newHealthPlugin(api)
	p.botUserID = ""

	w, body := doHealthRequest(p, testWebhookToken)

	assert.Equal(http.StatusServiceUnavailable, w.Code)
	assert.Equal("bot_account", body.Checks[0].Name)
	assert.Equal(healthStatusFail, body.Checks[0].Status)
}

// TestHealthProbeCached verifies that repeated uptime checks do not hammer the Better
// Stack API, and that rotating the API token invalidates the cached result.
func TestHealthProbeCached(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("GetChannel", "test-channel").Return(&model.Channel{Id: "test-channel"}, nil)

	calls := stubPing(t, nil)

	p := newHealthPlugin(api)

	for range 3 {
		w, _ := doHealthRequest(p, testWebhookToken)
		assert.Equal(http.StatusOK, w.Code)
	}
	assert.Equal(1, *calls, "expected the Better Stack probe to be cached")

	// A rotated API token must be probed again immediately.
	p.configuration = p.configuration.Clone()
	p.configuration.BetterStackUptimeAPIToken = "rotated-token"
	doHealthRequest(p, testWebhookToken)
	assert.Equal(2, *calls)

	// An expired cache entry is refreshed.
	p.healthProbedAt = time.Now().Add(-2 * healthProbeTTL)
	doHealthRequest(p, testWebhookToken)
	assert.Equal(3, *calls)
}

// TestHealthHeadRequest verifies that monitors configured to use HEAD still get a status code.
func TestHealthHeadRequest(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}
	api.On("GetChannel", "test-channel").Return(&model.Channel{Id: "test-channel"}, nil)

	stubPing(t, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodHead, healthURL(testWebhookToken), nil)
	newHealthPlugin(api).ServeHTTP(nil, w, r)

	assert.Equal(http.StatusOK, w.Code)
}

// TestHealthRejectsPost verifies the uptime check does not answer non-read methods.
func TestHealthRejectsPost(t *testing.T) {
	assert := assert.New(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, healthURL(testWebhookToken), nil)
	newHealthPlugin(&plugintest.API{}).ServeHTTP(nil, w, r)

	assert.Equal(http.StatusMethodNotAllowed, w.Code)
}
