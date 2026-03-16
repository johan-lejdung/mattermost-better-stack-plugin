package plugin

import (
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
//	POST /api/v1/webhook/{token}  — Better Stack incident webhook (no Mattermost auth, token in path)
func (p *BetterStackPlugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/webhook/{token}", p.handleWebhook).Methods(http.MethodPost)
	router.ServeHTTP(w, r)
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

	// --- Secret token validation ---
	token := mux.Vars(r)["token"]
	if config.WebhookToken == "" {
		p.API.LogError("Webhook received but token is not configured")
		http.Error(w, "Webhook not configured", http.StatusServiceUnavailable)
		return
	}
	if token != config.WebhookToken {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// --- Optional Basic Auth validation ---
	if config.WebhookUsername != "" && config.WebhookPassword != "" {
		username, password, ok := r.BasicAuth()
		if !ok || username != config.WebhookUsername || password != config.WebhookPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// --- Parse payload ---
	var payload WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		p.API.LogError("Failed to decode Better Stack webhook payload", "error", err.Error())
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

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
		Message:   payload.FormatOriginalPost(),
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
		logsReply := &model.Post{
			UserId:    p.botUserID,
			ChannelId: config.AlertChannelID,
			RootId:    created.Id,
			Message:   logsMessage,
		}
		if _, appErr := p.API.CreatePost(logsReply); appErr != nil {
			p.API.LogError("Failed to post surrounding logs reply", "incident_id", incidentID, "error", appErr.Error())
			// Non-fatal — the incident post was created successfully.
		}
	}

	w.WriteHeader(http.StatusOK)
}

// handleExistingIncident posts a thread reply and updates the status footer on the original post.
// If the original post no longer exists (e.g. it was deleted), it falls back to creating a new post.
func (p *BetterStackPlugin) handleExistingIncident(w http.ResponseWriter, config *configuration, payload *WebhookPayload, existingPostID string) {
	incidentID := payload.Data.ID

	// --- Verify the original post still exists ---
	originalPost, appErr := p.API.GetPost(existingPostID)
	if appErr != nil {
		p.API.LogWarn("Original incident post not found; creating a new post", "post_id", existingPostID, "incident_id", incidentID)
		p.handleNewIncident(w, config, payload, incidentID)
		return
	}

	// --- Post a thread reply with the status update ---
	reply := &model.Post{
		UserId:    p.botUserID,
		ChannelId: config.AlertChannelID,
		RootId:    existingPostID,
		Message:   payload.FormatThreadReply(),
	}

	if _, appErr := p.API.CreatePost(reply); appErr != nil {
		p.API.LogError("Failed to create thread reply", "root_post_id", existingPostID, "error", appErr.Error())
		// Continue so we still try to update the original post.
	}

	// --- Update the status footer on the original post ---
	originalPost.Message = payload.updateStatusFooter(originalPost.Message)

	if _, appErr := p.API.UpdatePost(originalPost); appErr != nil {
		p.API.LogError("Failed to update original incident post", "post_id", existingPostID, "error", appErr.Error())
		http.Error(w, "Failed to update post", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
