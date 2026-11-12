package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"partyline/internal/meter"
	"partyline/internal/player"
	"partyline/internal/sender"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Registered so the binding generator gives the UI typed events.
	application.RegisterEvent[player.State](eventState)
	application.RegisterEvent[[]player.AudioDevice](eventDevices)
	application.RegisterEvent[player.Levels](eventLevels)
	application.RegisterEvent[sender.State](eventSend)
	application.RegisterEvent[meter.Levels](eventSendLevels)
}

const singleInstanceID = "partyline-7c0d1f0e"

func main() {
	// The sender runs as a second copy of this program; see sender/worker.go.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case sender.WorkerArg:
			sender.RunWorker(os.Stdin, os.Stdout)
			return
		case sender.ListArg:
			sender.RunList(os.Stdout)
			return
		}
	}

	var window *application.WebviewWindow

	app := application.New(application.Options{
		Name:        appTitle,
		Description: "Low-latency audio player for VRCDN streams",
		Services: []application.Service{
			application.NewService(NewApp()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			AdditionalBrowserArgs: browserArgs(),
		},
		// Two copies would fight over the same audio device and config.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: instanceID(),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					window.Restore()
					window.Focus()
				}
			},
		},
	})

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     appTitle,
		Width:     880,
		Height:    620,
		MinWidth:  720,
		MinHeight: 480,
		// --gt-bg of the dark theme, so there's no bright flash while loading.
		BackgroundColour: application.NewRGB(26, 16, 51),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
