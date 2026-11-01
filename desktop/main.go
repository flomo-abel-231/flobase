// FloBase Desktop runs FloBase on a Mac (Intel or Apple silicon) or a
// Windows PC: it installs what FloBase needs, including a Docker engine,
// starts it, and opens the dashboard. It is a small window around the same
// local runner the flobase CLI uses (control-plane/local).
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "dev"

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:            "FloBase",
		Width:            760,
		Height:           560,
		MinWidth:         560,
		MinHeight:        460,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 10, G: 10, B: 10, A: 255},
		OnStartup:        app.startup,
		Bind:             []any{app},
		Mac: &mac.Options{
			About: &mac.AboutInfo{
				Title:   "FloBase Desktop",
				Message: "Version " + version + "\nOpen-source databases on your computer.",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
