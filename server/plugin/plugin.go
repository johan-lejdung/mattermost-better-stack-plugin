package plugin

import (
	"net/http"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/pkg/errors"

	"github.com/johan-lejdung/mattermost-better-stack-plugin/server/store/kvstore"
)

// BetterStackPlugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type BetterStackPlugin struct {
	plugin.MattermostPlugin

	// kvstore is the client used to read/write KV records for this plugin.
	kvstore kvstore.KVStore

	// client is the Mattermost server API client.
	client *pluginapi.Client

	// botUserID is the ID of the dedicated bot account used to post incident alerts.
	botUserID string

	backgroundJob *cluster.Job

	// healthLock guards the cached result of the uptime check's Better Stack API probe.
	healthLock       sync.Mutex
	healthProbeToken string
	healthProbedAt   time.Time
	healthProbeErr   error

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration
}

// OnActivate is invoked when the plugin is activated. If an error is returned, the plugin will be deactivated.
func (p *BetterStackPlugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)

	p.kvstore = kvstore.NewKVStore(p.client)

	botID, err := p.client.Bot.EnsureBot(&model.Bot{
		Username:    "betterstack",
		DisplayName: "BetterStack",
		Description: "Bot for Better Stack incident alerts and on-call information.",
	})
	if err != nil {
		return errors.Wrap(err, "failed to ensure BetterStack bot account")
	}
	p.botUserID = botID

	p.RegisterCommands()

	job, err := cluster.Schedule(
		p.API,
		"BackgroundJob",
		cluster.MakeWaitForRoundedInterval(10*time.Minute),
		p.runJob,
	)
	if err != nil {
		return errors.Wrap(err, "failed to schedule background job")
	}

	p.backgroundJob = job

	return nil
}

// OnDeactivate is invoked when the plugin is deactivated.
func (p *BetterStackPlugin) OnDeactivate() error {
	if p.backgroundJob != nil {
		if err := p.backgroundJob.Close(); err != nil {
			p.API.LogError("Failed to close background job", "err", err)
		}
	}
	return nil
}

// This will execute the commands that were registered in the NewCommandHandler function.
func (p *BetterStackPlugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	response, err := p.Handle(args)
	if err != nil {
		return nil, model.NewAppError("ExecuteCommand", "plugin.command.execute_command.app_error", nil, err.Error(), http.StatusInternalServerError)
	}
	return response, nil
}

// See https://developers.mattermost.com/extend/plugins/server/reference/
