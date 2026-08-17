package plugin

import (
	"encoding/json"
	"net/http"
	"time"
)

// healthProbeTTL is how long the result of the upstream Better Stack API probe is
// reused. Uptime monitors poll frequently (often every 30s), and without a cache
// every poll would spend a request against the Better Stack API rate limit.
const healthProbeTTL = 60 * time.Second

const (
	healthStatusOK   = "ok"
	healthStatusFail = "fail"
)

// pingBetterStack probes the Better Stack Uptime API with the given token. It is a
// package variable so tests can substitute it without making real network calls.
var pingBetterStack = func(token string) error {
	return newBetterStackClient(token).Ping()
}

// healthCheck is the result of a single dependency check.
type healthCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// healthResponse is the JSON body returned by the uptime check endpoint.
type healthResponse struct {
	Status string        `json:"status"`
	Checks []healthCheck `json:"checks"`
}

// handleHealth reports whether the plugin can actually do its job: the bot account
// exists, the alert channel is configured and readable, and the Better Stack API is
// reachable with the configured token.
//
// It returns 200 when every check passes and 503 when any of them fails, so an uptime
// monitor can alarm on the status code alone. The JSON body names the failing check for
// whoever investigates the alert.
//
// The endpoint is only reachable with the webhook secret token in the path, so it never
// exposes plugin state to unauthenticated callers. It deliberately does not enforce the
// webhook's optional Basic Auth: those credentials belong to Better Stack's incident
// posts, and an uptime monitor should not need them to poll this.
func (p *BetterStackPlugin) handleHealth(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()

	if !p.authorizeToken(w, r, config) {
		return
	}

	resp := healthResponse{
		Status: healthStatusOK,
		Checks: []healthCheck{
			p.checkBot(),
			p.checkAlertChannel(config),
			p.checkBetterStackAPI(config),
		},
	}

	statusCode := http.StatusOK
	for _, check := range resp.Checks {
		if check.Status != healthStatusOK {
			resp.Status = healthStatusFail
			statusCode = http.StatusServiceUnavailable
			break
		}
	}

	if resp.Status != healthStatusOK {
		p.API.LogWarn("Uptime check reported an unhealthy plugin", "checks", summarizeFailures(resp.Checks))
	}

	w.Header().Set("Content-Type", "application/json")
	// Uptime monitors must always see the live state, never a cached response.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		p.API.LogError("Failed to write uptime check response", "error", err.Error())
	}
}

// checkBot verifies that the bot account was resolved during activation. Without it
// the plugin cannot post anything.
func (p *BetterStackPlugin) checkBot() healthCheck {
	if p.botUserID == "" {
		return healthCheck{Name: "bot_account", Status: healthStatusFail, Detail: "bot account has not been created"}
	}
	return healthCheck{Name: "bot_account", Status: healthStatusOK}
}

// checkAlertChannel verifies that an alert channel is configured and still readable.
// A deleted or renamed-away channel would silently swallow every incident post.
func (p *BetterStackPlugin) checkAlertChannel(config *configuration) healthCheck {
	if config.AlertChannelID == "" {
		return healthCheck{Name: "alert_channel", Status: healthStatusFail, Detail: "alert channel is not configured"}
	}

	if _, appErr := p.API.GetChannel(config.AlertChannelID); appErr != nil {
		return healthCheck{Name: "alert_channel", Status: healthStatusFail, Detail: "alert channel could not be read: " + appErr.Error()}
	}

	return healthCheck{Name: "alert_channel", Status: healthStatusOK}
}

// checkBetterStackAPI verifies that the Better Stack Uptime API answers with the
// configured token. The result is cached for healthProbeTTL — see probeBetterStackAPI.
func (p *BetterStackPlugin) checkBetterStackAPI(config *configuration) healthCheck {
	if config.BetterStackUptimeAPIToken == "" {
		return healthCheck{Name: "better_stack_api", Status: healthStatusFail, Detail: "Better Stack API token is not configured"}
	}

	if err := p.probeBetterStackAPI(config.BetterStackUptimeAPIToken); err != nil {
		return healthCheck{Name: "better_stack_api", Status: healthStatusFail, Detail: err.Error()}
	}

	return healthCheck{Name: "better_stack_api", Status: healthStatusOK}
}

// probeBetterStackAPI calls the Better Stack API at most once per healthProbeTTL and
// returns the cached outcome in between. The cache is keyed on the token so rotating it
// in System Console takes effect immediately.
//
// The lock is deliberately held across the HTTP call: concurrent uptime checks then wait
// for the in-flight probe instead of each firing their own request.
func (p *BetterStackPlugin) probeBetterStackAPI(token string) error {
	p.healthLock.Lock()
	defer p.healthLock.Unlock()

	if token == p.healthProbeToken && !p.healthProbedAt.IsZero() && time.Since(p.healthProbedAt) < healthProbeTTL {
		return p.healthProbeErr
	}

	err := pingBetterStack(token)

	p.healthProbeToken = token
	p.healthProbedAt = time.Now()
	p.healthProbeErr = err

	return err
}

// summarizeFailures renders the failing checks as a single log-friendly string.
func summarizeFailures(checks []healthCheck) string {
	var summary string
	for _, check := range checks {
		if check.Status == healthStatusOK {
			continue
		}
		if summary != "" {
			summary += "; "
		}
		summary += check.Name + ": " + check.Detail
	}
	return summary
}
