package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "dev"

func main() {
	app := NewApp()
	if err := wails.Run(&options.App{
		Title:         "KubeLoop",
		Width:         1080,
		Height:        720,
		MinWidth:      1080,
		MinHeight:     720,
		MaxWidth:      1080,
		MaxHeight:     720,
		DisableResize: true,
		Frameless:     true,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "dev.fengqi.kube-loop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx != nil {
					wailsruntime.WindowUnminimise(app.ctx)
					wailsruntime.WindowShow(app.ctx)
				}
			},
		},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 1},
	}); err != nil {
		log.Fatal(err)
	}
}
