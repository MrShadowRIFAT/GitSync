package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "GitSync",
		Width:            1140,
		Height:           740,
		MinWidth:         900,
		MinHeight:        600,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 23, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:     app.Startup,
		OnDomReady:    app.domReady,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		Bind: []interface{}{
			app,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "gitsync-desktop-single-instance-v1",
			OnSecondInstanceLaunch: app.onSecondInstanceLaunch,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
	})
	if err != nil {
		println("Error starting GitSync:", err.Error())
	}
}
