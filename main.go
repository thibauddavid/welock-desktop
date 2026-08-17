package main

import (
	"log"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	appcore "github.com/thibauddavid/welock-desktop/internal/app"
	"github.com/thibauddavid/welock-desktop/internal/ui"
)

// main wires the Fyne application to the backend Core: it loads any persisted
// session, then shows the login screen or the master-detail main window. Login
// success and logout / auth-failure swap the single root window's content.
func main() {
	a := fyneapp.NewWithID("com.welock.desktop")
	a.Settings().SetTheme(ui.NewTheme())

	w := a.NewWindow("WeLock")
	w.Resize(fyne.NewSize(ui.DefaultWidth, ui.DefaultHeight))
	w.CenterOnScreen()

	core, err := appcore.New("")
	if err != nil {
		w.SetContent(container.NewCenter(widget.NewLabel("Failed to initialize: " + err.Error())))
		w.ShowAndRun()
		return
	}

	// Anonymous product telemetry: the version comes from the packaged app metadata
	// (FyneApp.toml); app_opened drives the #users/retention metrics. No-op in dev builds
	// and when the user has opted out (see internal/analytics).
	core.SetAppVersion(a.Metadata().Version)
	core.Track("app_opened", nil)

	var showLogin, showMain func()
	showLogin = func() {
		w.SetContent(ui.NewLogin(a, w, core, func() { core.Track("login", nil); showMain() }))
	}
	showMain = func() {
		w.SetContent(ui.NewMainWindow(a, w, core, func() { showLogin() }))
	}

	if core.LoggedIn() {
		showMain()
	} else {
		showLogin()
	}

	log.SetPrefix("welock-desktop: ")
	w.ShowAndRun()
}
