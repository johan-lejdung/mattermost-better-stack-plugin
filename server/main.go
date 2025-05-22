package main

import (
	"github.com/mattermost/mattermost/server/public/plugin"

	pbs "github.com/johan-lejdung/mattermost-better-stack-plugin/server/plugin"
)

func main() {
	plugin.ClientMain(&pbs.BetterStackPlugin{})
}
