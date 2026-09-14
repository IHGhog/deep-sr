package main

import (
	"embed"
	"os"
	"path/filepath"

	"deepsr/internal/engine/cmdutil"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cleanupLock, ok := AcquireSingleInstance()
	if !ok {
		// 已有实例在运行，已唤醒前台窗口并退出
		return
	}
	defer cleanupLock()

	// 初始化 Windows Job Object，绑定当前进程与全部衍生子进程树，保证宿主退出时内核级自动销毁
	cmdutil.InitProcessJob()

	app := NewApp()

	var webviewUserDataPath string
	if userConfigDir, err := os.UserConfigDir(); err == nil {
		webviewUserDataPath = filepath.Join(userConfigDir, "DeepSR")
	}

	err := wails.Run(&options.App{
		Title:            "DeepSR - AI 画质超分辨率增强",
		Width:            1200,
		Height:           800,
		MinWidth:         960,
		MinHeight:        650,
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1}, // #F8FAFC
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
		},
		OnStartup:     app.startup,
		OnDomReady:    app.domReady,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			BackdropType:         windows.None,
			Theme:                windows.SystemDefault,
			WebviewUserDataPath:  webviewUserDataPath,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
