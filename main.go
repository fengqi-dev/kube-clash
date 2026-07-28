package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	if err := wails.Run(&options.App{
		Title:            "Kube Clash",
		Width:            1080,
		Height:           720,
		MinWidth:         840,
		MinHeight:        580,
		Frameless:        false,
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 1},
	}); err != nil {
		log.Fatal(err)
	}
}
