package plugin

import (
	"fmt"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// newTestPlugin builds a BetterStackPlugin suitable for unit tests without calling OnActivate,
// since OnActivate requires a live Mattermost server (bot registration, cluster scheduling, etc.).
func newTestPlugin(api *plugintest.API) *BetterStackPlugin {
	p := &BetterStackPlugin{}
	p.SetAPI(api)
	p.configuration = &configuration{
		BetterStackUptimeAPIToken: "test-token",
		AlertChannelID:            "test-channel-id",
	}
	return p
}

func TestNoSubcommand(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}

	api.On("GetBundlePath").Return("", fmt.Errorf("no file found"))
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything)

	p := newTestPlugin(api)

	args := &model.CommandArgs{
		Command: "/betterstack",
	}
	response, err := p.Handle(args)
	assert.Nil(err)
	assert.Equal(model.CommandResponseTypeEphemeral, response.ResponseType)
	assert.Contains(response.Text, "Usage:")
}

func TestUnknownSubcommand(t *testing.T) {
	assert := assert.New(t)
	api := &plugintest.API{}

	api.On("GetBundlePath").Return("", fmt.Errorf("no file found"))
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything)

	p := newTestPlugin(api)

	args := &model.CommandArgs{
		Command: "/betterstack unknowncommand",
	}
	response, err := p.Handle(args)
	assert.Nil(err)
	assert.Equal(model.CommandResponseTypeEphemeral, response.ResponseType)
	assert.Contains(response.Text, "Unknown subcommand")
}
