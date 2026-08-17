package plugin

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

// ServeHTTP routes incoming HTTP requests to the plugin.
// The plugin base URL is: <siteUrl>/plugins/com.mattermost.plugin-better-stack/
//
// Routes:
//
//	POST     /api/v1/webhook/{token}  — Better Stack incident webhook (no Mattermost auth, token in path)
//	GET|HEAD /api/v1/health/{token}   — uptime check for monitoring this plugin (same token)
func (p *BetterStackPlugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/webhook/{token}", p.handleWebhook).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/health/{token}", p.handleHealth).Methods(http.MethodGet, http.MethodHead)
	router.ServeHTTP(w, r)
}

// authorizeSecret validates the secret token in the URL path and, when configured, the
// Basic Auth credentials. It writes the error response and returns false if the request
// is not authorized.
func (p *BetterStackPlugin) authorizeSecret(w http.ResponseWriter, r *http.Request, config *configuration) bool {
	if config.WebhookToken == "" {
		p.API.LogError("Request received but webhook token is not configured", "path", r.URL.Path)
		http.Error(w, "Webhook not configured", http.StatusServiceUnavailable)
		return false
	}

	if subtle.ConstantTimeCompare([]byte(mux.Vars(r)["token"]), []byte(config.WebhookToken)) != 1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	if config.WebhookUsername != "" && config.WebhookPassword != "" {
		username, password, ok := r.BasicAuth()
		if !ok || username != config.WebhookUsername || password != config.WebhookPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return false
		}
	}

	return true
}

// handleWebhook receives Better Stack incident webhook payloads and:
//  1. Validates the secret token in the URL path.
//  2. Validates Basic Auth credentials if configured.
//  3. Parses the payload.
//  4. Looks up the existing Mattermost post for this incident (via KV store).
//  5. If found: posts a thread reply with the new status and updates the original post footer.
//  6. If not found: creates a new post in the configured alert channel and stores the mapping.
func (p *BetterStackPlugin) handleWebhook(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()

	// --- Secret token and optional Basic Auth validation ---
	if !p.authorizeSecret(w, r, config) {
		return
	}

	// --- Parse payload ---
	var payload WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		p.API.LogError("Failed to decode Better Stack webhook payload", "error", err.Error())
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Better Stack sends template variables it cannot fill verbatim (e.g. "$RESOLVED_BY"
	// on an alarm); treat those as absent rather than rendering them into posts.
	payload.normalize()

	incidentID := payload.Data.ID
	if incidentID == "" {
		p.API.LogError("Better Stack webhook payload missing incident ID")
		http.Error(w, "Bad request: missing incident ID", http.StatusBadRequest)
		return
	}

	if config.AlertChannelID == "" {
		p.API.LogError("Alert channel not configured; cannot post incident", "incident_id", incidentID)
		http.Error(w, "Plugin misconfigured", http.StatusInternalServerError)
		return
	}

	// --- Look up existing post ---
	existingPostID, err := p.kvstore.GetIncidentPost(incidentID)
	if err != nil {
		p.API.LogError("Failed to look up incident post mapping", "incident_id", incidentID, "error", err.Error())
		// Non-fatal: fall through to creating a new post.
	}

	if existingPostID != "" {
		p.handleExistingIncident(w, config, &payload, existingPostID)
		return
	}

	p.handleNewIncident(w, config, &payload, incidentID)
}

// handleNewIncident creates the initial post for an incident we haven't seen before.
func (p *BetterStackPlugin) handleNewIncident(w http.ResponseWriter, config *configuration, payload *WebhookPayload, incidentID string) {
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: config.AlertChannelID,
		Message:   payload.FormatOriginalPost(payload.Status()),
		Props: model.StringInterface{
			"betterstack_incident_id": incidentID,
		},
	}

	created, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.API.LogError("Failed to create incident post", "incident_id", incidentID, "error", appErr.Error())
		http.Error(w, "Failed to create post", http.StatusInternalServerError)
		return
	}

	if err := p.kvstore.StoreIncidentPost(incidentID, created.Id); err != nil {
		p.API.LogError("Failed to store incident→post mapping", "incident_id", incidentID, "post_id", created.Id, "error", err.Error())
		// Don't fail the request — the post was created successfully.
	}

	// If surrounding logs were included, post them as a thread reply so they
	// don't dominate the root post.
	if logsMessage := payload.FormatLogsReply(); logsMessage != "" {
		p.postThreadReply(config, created.Id, logsMessage, "surrounding logs", incidentID)
	}

	// An incident we have never posted about can still arrive as a comment event, e.g.
	// if the alarm webhook was missed.
	if commentMessage := payload.FormatCommentReply(); commentMessage != "" {
		p.postThreadReply(config, created.Id, commentMessage, "comment", incidentID)
	}

	w.WriteHeader(http.StatusOK)
}

// handleExistingIncident posts a thread reply for the event and re-renders the original
// post so its body always reflects the incident's current state. If the original post no
// longer exists (e.g. it was deleted), it falls back to creating a new post.
func (p *BetterStackPlugin) handleExistingIncident(w http.ResponseWriter, config *configuration, payload *WebhookPayload, existingPostID string) {
	incidentID := payload.Data.ID

	// --- Verify the original post still exists ---
	originalPost, appErr := p.API.GetPost(existingPostID)
	if appErr != nil {
		p.API.LogWarn("Original incident post not found; creating a new post", "post_id", existingPostID, "incident_id", incidentID)
		p.handleNewIncident(w, config, payload, incidentID)
		return
	}

	// The status the post currently shows tells us whether this alarm is a reopen.
	status := payload.EffectiveStatus(previousStatusFromPost(originalPost.Message))

	// --- Post a thread reply for this event ---
	// A comment does not change the incident status, so it replaces the status update
	// rather than being posted alongside a repeat of the current status.
	if payload.IsComment() {
		p.postThreadReply(config, existingPostID, payload.FormatCommentReply(), "comment", incidentID)
	} else {
		p.postThreadReply(config, existingPostID, payload.FormatThreadReply(status), "status update", incidentID)
	}

	// --- Re-render the original post from the current payload ---
	// Rewriting the whole body (rather than only the status footer) keeps the
	// acknowledged and resolved lines correct when the incident is reopened and then
	// acknowledged or resolved by someone else.
	originalPost.Message = payload.FormatOriginalPost(status)

	if _, appErr := p.API.UpdatePost(originalPost); appErr != nil {
		p.API.LogError("Failed to update original incident post", "post_id", existingPostID, "error", appErr.Error())
		http.Error(w, "Failed to update post", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// postThreadReply posts a reply in an incident's thread. Failures are logged but never
// fail the webhook — the root post is the important part.
func (p *BetterStackPlugin) postThreadReply(config *configuration, rootID, message, kind, incidentID string) {
	if message == "" {
		return
	}

	reply := &model.Post{
		UserId:    p.botUserID,
		ChannelId: config.AlertChannelID,
		RootId:    rootID,
		Message:   message,
	}

	if _, appErr := p.API.CreatePost(reply); appErr != nil {
		p.API.LogError("Failed to post incident thread reply", "kind", kind, "root_post_id", rootID, "incident_id", incidentID, "error", appErr.Error())
	}
}
