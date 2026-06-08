// Nawala Checker — cek status blokir domain di TrustPositif (Komdigi/Kominfo).
package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/l0rd/nawala-checker/internal/cli"
)

//go:embed all:frontend/dist
var assets embed.FS

var (
	Version   = "1.0.0"
	Developer = "willygoid"
)

func main() {
	args := os.Args[1:]

	// Jika ada argumen "gui", paksakan GUI.
	if len(args) == 1 && args[0] == "gui" {
		runGUI()
		return
	}

	// Tanpa argumen: GUI jika interaktif, CLI jika stdin dipipe.
	if len(args) == 0 {
		if st, _ := os.Stdin.Stat(); st != nil && (st.Mode()&os.ModeCharDevice) == 0 {
			os.Exit(cli.Run(args, Version, Developer))
		}
		runGUI()
		return
	}

	os.Exit(cli.Run(args, Version, Developer))
}

func runGUI() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  fmt.Sprintf("Nawala Checker v%s", Version),
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 245, G: 247, B: 250, A: 255}, // Light clean color
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			Appearance:           mac.NSAppearanceNameAqua, // Force Light Mode
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
		},
	})

	if err != nil {
		fmt.Println("Error:", err.Error())
	}
}
