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
		Title: appTitle,
		// Sized for the middle window with both wings open; the UI narrows
		// it when a wing is folded.
		Width:     1122,
		Height:    660,
		MinWidth:  560,
		MinHeight: 520,
		// No Windows title bar: the app draws its own, with its own minimise,
		// maximise and close buttons, and a rounded border around the lot.
		// The window itself is see-through so the corners outside that
		// border show whatever is behind the app.
		Frameless:        true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Windows: application.WindowsWindow{
			// Lets the CSS "app-region: drag" areas act as a real title
			// bar, so Windows itself moves the window. Without it only a
			// mouse can drag: touch, pen and VR controller pointers that
			// act as touch never produce the mouse moves Wails listens for.
			NonClientRegionSupport: true,
		},
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
