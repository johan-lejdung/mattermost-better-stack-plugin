package plugin

import (
	"reflect"

	"github.com/pkg/errors"
)

// configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration, as well as values computed from the configuration. Any public fields will be
// deserialized from the Mattermost server configuration in OnConfigurationChange.
//
// As plugins are inherently concurrent (hooks being called asynchronously), and the plugin
// configuration can change at any time, access to the configuration must be synchronized. The
// strategy used in this plugin is to guard a pointer to the configuration, and clone the entire
// struct whenever it changes. You may replace this with whatever strategy you choose.
//
// If you add non-reference types to your configuration struct, be sure to rewrite Clone as a deep
// copy appropriate for your types.
type configuration struct {
	// BetterStackUptimeAPIToken is used to authenticate with the Better Stack Uptime API
	// for slash commands (on-call schedules, incidents).
	BetterStackUptimeAPIToken string `json:"betterStackUptimeAPIToken"`

	// AlertChannelID is the Mattermost channel ID where incident alert posts are created.
	AlertChannelID string `json:"alertChannelId"`

	// WebhookToken is a generated secret token that forms part of the webhook URL path,
	// e.g. /api/v1/webhook/<token>. Requests with an incorrect token are rejected with 401.
	// Mattermost auto-generates this value and provides a "Regenerate" button in System Console.
	WebhookToken string `json:"webhookToken"`

	// WebhookUsername and WebhookPassword are optional Basic Auth credentials validated on
	// incoming Better Stack webhook requests. When both are empty, Basic Auth is not enforced.
	WebhookUsername string `json:"webhookUsername"`
	WebhookPassword string `json:"webhookPassword"`

	// DailyOncallDigest controls whether a daily on-call digest is posted to the alert
	// channel at 08:00 CEST regardless of whether the on-call person has changed.
	DailyOncallDigest bool `json:"dailyOncallDigest"`
}

// Clone shallow copies the configuration. Your implementation may require a deep copy if
// your configuration has reference types.
func (c *configuration) Clone() *configuration {
	var clone = *c
	return &clone
}

// getConfiguration retrieves the active configuration under lock, making it safe to use
// concurrently. The active configuration may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *BetterStackPlugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
//
// Do not call setConfiguration while holding the configurationLock, as sync.Mutex is not
// reentrant. In particular, avoid using the plugin API entirely, as this may in turn trigger a
// hook back into the plugin. If that hook attempts to acquire this lock, a deadlock may occur.
//
// This method panics if setConfiguration is called with the existing configuration. This almost
// certainly means that the configuration was modified without being cloned and may result in
// an unsafe access.
func (p *BetterStackPlugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		// Ignore assignment if the configuration struct is empty. Go will optimize the
		// allocation for same to point at the same memory address, breaking the check
		// above.
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}

		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *BetterStackPlugin) OnConfigurationChange() error {
	var configuration = new(configuration)

	// Load the public configuration fields from the Mattermost server configuration.
	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	// All fields are warn-only at config load time so that the first save in System Console
	// succeeds (allowing Mattermost to generate the webhook token). Missing fields are enforced
	// at runtime in the relevant handlers instead.
	if configuration.BetterStackUptimeAPIToken == "" {
		p.API.LogWarn("Better Stack API token is not set. Slash commands will not work until configured.")
	}

	if configuration.AlertChannelID == "" {
		p.API.LogWarn("Alert channel ID is not set. Incoming webhooks will not post until configured.")
	}

	if configuration.WebhookToken == "" {
		p.API.LogWarn("Webhook token not yet generated. Save the plugin settings in System Console.")
	}

	// Basic Auth is optional but must be all-or-nothing — this is the one hard error since
	// a half-configured Basic Auth would silently accept or reject all webhooks.
	if (configuration.WebhookUsername == "") != (configuration.WebhookPassword == "") {
		return errors.New("both webhook username and password must be set, or both left empty")
	}

	p.setConfiguration(configuration)

	return nil
}
