package plugin

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

var (
	commandTrigger          = "betterstack"
	commandAutocompleteDesc = "Available commands: " + oncallTrigger + " | " + incidentsTrigger + " | " + monitorsTrigger + " | " + statusTrigger
	commandAutocompleteHint = "[command]"

	oncallTrigger  = "oncall"
	oncallHint     = ""
	oncallHelpText = "Show on-call schedules and who is currently on call"

	incidentsTrigger  = "incidents"
	incidentsHint     = ""
	incidentsHelpText = "List currently active (unresolved) Better Stack incidents"

	monitorsTrigger  = "monitors"
	monitorsHint     = "[all|down]"
	monitorsHelpText = "List monitors and their status. Use 'all' for all monitors, 'down' for failing only"

	statusTrigger  = "status"
	statusHint     = ""
	statusHelpText = "List Better Stack status pages and their current aggregate state"
)

func getAutocompleteData() *model.AutocompleteData {
	mainCommand := model.NewAutocompleteData(commandTrigger, commandAutocompleteHint, commandAutocompleteDesc)
	mainCommand.AddCommand(getOncallAutocompleteData())
	mainCommand.AddCommand(getIncidentsAutocompleteData())
	mainCommand.AddCommand(getMonitorsAutocompleteData())
	mainCommand.AddCommand(getStatusAutocompleteData())
	return mainCommand
}

func getOncallAutocompleteData() *model.AutocompleteData {
	return model.NewAutocompleteData(oncallTrigger, oncallHint, oncallHelpText)
}

func getIncidentsAutocompleteData() *model.AutocompleteData {
	return model.NewAutocompleteData(incidentsTrigger, incidentsHint, incidentsHelpText)
}

func getMonitorsAutocompleteData() *model.AutocompleteData {
	cmd := model.NewAutocompleteData(monitorsTrigger, monitorsHint, monitorsHelpText)
	cmd.AddCommand(model.NewAutocompleteData("all", "", "Show all monitors"))
	cmd.AddCommand(model.NewAutocompleteData("down", "", "Show only monitors that are currently down"))
	return cmd
}

func getStatusAutocompleteData() *model.AutocompleteData {
	return model.NewAutocompleteData(statusTrigger, statusHint, statusHelpText)
}

func getAutocompleteIconData(api plugin.API) string {
	bundlePath, err := api.GetBundlePath()
	if err != nil {
		api.LogError("Couldn't get bundle path", "error", err)
		return ""
	}

	icon, err := os.ReadFile(filepath.Join(bundlePath, "assets", "betterstack_logo.jpg"))
	if err != nil {
		api.LogError("Failed to open icon", "error", err)
		return ""
	}

	// The icon is a JPEG but we encode it as base64 for the autocomplete data URI.
	return fmt.Sprintf("data:image/jpeg;base64,%s", base64.StdEncoding.EncodeToString(icon))
}

// RegisterCommands registers the /betterstack slash command with Mattermost.
func (p *BetterStackPlugin) RegisterCommands() {
	err := p.client.SlashCommand.Register(&model.Command{
		Trigger:              commandTrigger,
		AutoComplete:         true,
		AutoCompleteHint:     commandAutocompleteHint,
		AutoCompleteDesc:     commandAutocompleteDesc,
		AutocompleteData:     getAutocompleteData(),
		AutocompleteIconData: getAutocompleteIconData(p.API),
	})
	if err != nil {
		p.client.Log.Error("Failed to register command", "error", err)
	}
}

// Handle dispatches slash command invocations to the appropriate sub-handler.
func (p *BetterStackPlugin) Handle(args *model.CommandArgs) (*model.CommandResponse, error) {
	trigger := strings.TrimPrefix(strings.Fields(args.Command)[0], "/")
	switch trigger {
	case commandTrigger:
		return p.executeCommand(args), nil
	default:
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         fmt.Sprintf("Unknown command: %s", args.Command),
		}, nil
	}
}

func (p *BetterStackPlugin) executeCommand(args *model.CommandArgs) *model.CommandResponse {
	fields := strings.Fields(args.Command)
	if len(fields) < 2 {
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         fmt.Sprintf("Usage: `/%s [%s]`", commandTrigger, commandAutocompleteDesc),
		}
	}

	switch fields[1] {
	case oncallTrigger:
		return p.executeOncall()
	case incidentsTrigger:
		return p.executeIncidents()
	case monitorsTrigger:
		if len(fields) < 3 {
			return &model.CommandResponse{
				ResponseType: model.CommandResponseTypeEphemeral,
				Text:         fmt.Sprintf("Usage: `/%s %s %s`", commandTrigger, monitorsTrigger, monitorsHint),
			}
		}
		switch fields[2] {
		case "all":
			return p.executeMonitors(false)
		case "down":
			return p.executeMonitors(true)
		default:
			return &model.CommandResponse{
				ResponseType: model.CommandResponseTypeEphemeral,
				Text:         fmt.Sprintf("Unknown option `%s`. Usage: `/%s %s %s`", fields[2], commandTrigger, monitorsTrigger, monitorsHint),
			}
		}
	case statusTrigger:
		return p.executeStatus()
	default:
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         fmt.Sprintf("Unknown subcommand `%s`. %s", fields[1], commandAutocompleteDesc),
		}
	}
}

// executeOncall fetches on-call schedules from Better Stack and formats them for display.
func (p *BetterStackPlugin) executeOncall() *model.CommandResponse {
	config := p.getConfiguration()
	if config.BetterStackUptimeAPIToken == "" {
		return ephemeralError("Better Stack API token is not configured.")
	}

	client := newBetterStackClient(config.BetterStackUptimeAPIToken)
	schedules, userDetails, err := client.GetOnCallSchedules()
	if err != nil {
		p.API.LogError("Failed to fetch on-call schedules", "error", err.Error())
		return ephemeralError("Failed to fetch on-call schedules from Better Stack: " + err.Error())
	}

	if len(schedules) == 0 {
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         "No on-call schedules found.",
		}
	}

	var sb strings.Builder
	sb.WriteString("### On-call schedules\n\n")

	for _, schedule := range schedules {
		name := schedule.Attributes.Name
		if name == "" {
			name = "Default"
		}
		if schedule.Attributes.TeamName != "" {
			name = schedule.Attributes.TeamName + " — " + name
		}

		sb.WriteString(fmt.Sprintf("**%s**", name))
		if schedule.Attributes.DefaultCalendar {
			sb.WriteString(" _(default)_")
		}
		sb.WriteString("\n")

		onCallUsers := schedule.Relationships.OnCallUsers.Data
		if len(onCallUsers) == 0 {
			sb.WriteString("  - _Nobody currently on call_\n")
		} else {
			for _, u := range onCallUsers {
				detail, ok := userDetails[u.ID]
				if ok {
					name := strings.TrimSpace(detail.Attributes.FirstName + " " + detail.Attributes.LastName)
					sb.WriteString(fmt.Sprintf("  - %s <%s>\n", name, detail.Attributes.Email))
				} else {
					sb.WriteString(fmt.Sprintf("  - User ID %s <%s>\n", u.ID, u.Meta.Email))
				}
			}
		}
		sb.WriteString("\n")
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

// executeIncidents fetches active (unresolved) incidents from Better Stack.
func (p *BetterStackPlugin) executeIncidents() *model.CommandResponse {
	config := p.getConfiguration()
	if config.BetterStackUptimeAPIToken == "" {
		return ephemeralError("Better Stack API token is not configured.")
	}

	client := newBetterStackClient(config.BetterStackUptimeAPIToken)
	incidents, err := client.GetActiveIncidents()
	if err != nil {
		p.API.LogError("Failed to fetch active incidents", "error", err.Error())
		return ephemeralError("Failed to fetch incidents from Better Stack: " + err.Error())
	}

	if len(incidents) == 0 {
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         ":white_check_mark: No active incidents.",
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Active incidents (%d)\n\n", len(incidents)))

	for _, inc := range incidents {
		attrs := inc.Attributes

		// Format the incident URL as a link if available.
		nameField := attrs.Name
		if attrs.URL != "" {
			nameField = fmt.Sprintf("[%s](%s)", attrs.Name, attrs.URL)
		}

		statusIcon := ":red_circle:"
		if attrs.AcknowledgedAt != nil {
			statusIcon = ":bell:"
		}

		sb.WriteString(fmt.Sprintf("%s **%s**\n", statusIcon, nameField))
		sb.WriteString(fmt.Sprintf("  Cause: %s\n", attrs.Cause))
		sb.WriteString(fmt.Sprintf("  Started: %s\n", attrs.StartedAt))

		if attrs.AcknowledgedAt != nil {
			ackBy := ""
			if attrs.AcknowledgedBy != nil {
				ackBy = " by " + *attrs.AcknowledgedBy
			}
			sb.WriteString(fmt.Sprintf("  Acknowledged%s at %s\n", ackBy, *attrs.AcknowledgedAt))
		}
		if attrs.TeamName != "" {
			sb.WriteString(fmt.Sprintf("  Team: %s\n", attrs.TeamName))
		}
		sb.WriteString("\n")
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

// executeMonitors fetches monitors from Better Stack, optionally filtered to down-only.
func (p *BetterStackPlugin) executeMonitors(onlyDown bool) *model.CommandResponse {
	config := p.getConfiguration()
	if config.BetterStackUptimeAPIToken == "" {
		return ephemeralError("Better Stack API token is not configured.")
	}

	statusFilter := ""
	if onlyDown {
		statusFilter = "down"
	}

	client := newBetterStackClient(config.BetterStackUptimeAPIToken)
	monitors, err := client.GetMonitors(statusFilter)
	if err != nil {
		p.API.LogError("Failed to fetch monitors", "error", err.Error())
		return ephemeralError("Failed to fetch monitors from Better Stack: " + err.Error())
	}

	if len(monitors) == 0 {
		if onlyDown {
			return &model.CommandResponse{
				ResponseType: model.CommandResponseTypeEphemeral,
				Text:         ":white_check_mark: No monitors are currently down.",
			}
		}
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         "No monitors found.",
		}
	}

	title := fmt.Sprintf("### Monitors (%d)\n\n", len(monitors))
	if onlyDown {
		title = fmt.Sprintf("### Monitors down (%d)\n\n", len(monitors))
	}

	var sb strings.Builder
	sb.WriteString(title)

	statusIcons := map[string]string{
		"up":          ":white_check_mark:",
		"down":        ":red_circle:",
		"paused":      ":pause_button:",
		"maintenance": ":wrench:",
		"pending":     ":hourglass:",
		"validating":  ":mag:",
	}

	for _, m := range monitors {
		attrs := m.Attributes
		icon := statusIcons[attrs.Status]
		if icon == "" {
			icon = ":grey_question:"
		}

		name := attrs.PronouncableName
		if name == "" {
			name = attrs.URL
		}

		sb.WriteString(fmt.Sprintf("%s **%s** — `%s`\n", icon, name, attrs.Status))
		sb.WriteString(fmt.Sprintf("  URL: %s\n", attrs.URL))
		if attrs.TeamName != "" {
			sb.WriteString(fmt.Sprintf("  Team: %s\n", attrs.TeamName))
		}
		if attrs.LastCheckedAt != "" {
			sb.WriteString(fmt.Sprintf("  Last checked: %s\n", attrs.LastCheckedAt))
		}
		sb.WriteString("\n")
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

// executeStatus fetches status pages from Better Stack and displays their aggregate state.
func (p *BetterStackPlugin) executeStatus() *model.CommandResponse {
	config := p.getConfiguration()
	if config.BetterStackUptimeAPIToken == "" {
		return ephemeralError("Better Stack API token is not configured.")
	}

	client := newBetterStackClient(config.BetterStackUptimeAPIToken)
	pages, err := client.GetStatusPages()
	if err != nil {
		p.API.LogError("Failed to fetch status pages", "error", err.Error())
		return ephemeralError("Failed to fetch status pages from Better Stack: " + err.Error())
	}

	if len(pages) == 0 {
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         "No status pages found.",
		}
	}

	stateIcons := map[string]string{
		"operational":          ":white_check_mark:",
		"degraded_performance": ":warning:",
		"partial_outage":       ":large_orange_circle:",
		"major_outage":         ":red_circle:",
		"under_maintenance":    ":wrench:",
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Status pages (%d)\n\n", len(pages)))

	for _, sp := range pages {
		attrs := sp.Attributes
		icon := stateIcons[attrs.AggregateState]
		if icon == "" {
			icon = ":grey_question:"
		}

		name := attrs.CompanyName
		if name == "" {
			name = attrs.Subdomain
		}

		domain := attrs.CustomDomain
		if domain == "" {
			domain = attrs.Subdomain + ".betteruptime.com"
		}

		sb.WriteString(fmt.Sprintf("%s **%s** — `%s`\n", icon, name, attrs.AggregateState))
		sb.WriteString(fmt.Sprintf("  URL: https://%s\n", domain))
		sb.WriteString("\n")
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

// ephemeralError returns an ephemeral command response with an error message.
func ephemeralError(msg string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         ":warning: " + msg,
	}
}
