//go:build windows

// The Wails front end for MC Tunnel. The Win32 window in internal/ui still
// builds from cmd/mctunnel; this binary is the replacement being trialled on
// the dev/gui branch.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// Window geometry. The design is laid out for exactly this client area: it is
// fixed, because resizing would break the two-column layout.
const (
	windowWidth  = 960
	windowHeight = 440
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := newApp()

	err := wails.Run(&options.App{
		Title:         "MC Tunnel",
		Width:         windowWidth,
		Height:        windowHeight,
		MinWidth:      windowWidth,
		MaxWidth:      windowWidth,
		MinHeight:     windowHeight,
		MaxHeight:     windowHeight,
		DisableResize: true,
		// The page draws its own title bar, so the OS one is hidden. The bar is
		// marked draggable in styles.css.
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 8, G: 8, B: 17, A: 1},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		Bind:          []any{app},
		Windows: &windows.Options{
			// A named class lets the window be found again for shaping, without
			// matching on the title text.
			WindowClassName: windowClass,
		},
	})
	if err != nil {
		panic(err)
	}
}
