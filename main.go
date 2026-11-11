package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     appTitle,
		Width:     880,
		Height:    620,
		MinWidth:  720,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// Matches --bg in the stylesheet so there's no flash while loading.
		BackgroundColour: &options.RGBA{R: 26, G: 16, B: 51, A: 1}, // --gt-bg of the dark theme
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		// Two copies would fight over the same audio device and config.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "vrcdn-lowlatency-player-7c0d1f0e",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx != nil {
					runtime.WindowUnminimise(app.ctx)
					runtime.Show(app.ctx)
				}
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
