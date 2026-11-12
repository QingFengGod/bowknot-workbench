package main

import (
	"log"

	"bowknot-workbench/internal/backend"
	"bowknot-workbench/internal/ui/shell"

	"github.com/egoist/mygo"
)

func main() {
	app := backend.NewApp("0.0.1")
	plugins := backend.NewPluginService()
	appCatalog := backend.NewAppCatalog()
	assistant := backend.NewAssistantService()
	agents := backend.NewAgentService()
	desktop := shell.New(shell.Deps{
		App:       app,
		Plugins:   plugins,
		Apps:      appCatalog,
		Assistant: assistant,
		Agents:    agents,
	})

	mygo.App.WhenReady(func() {
		desktop.Start()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
