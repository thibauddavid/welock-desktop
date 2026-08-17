package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// newSettings renders the read-only account/session card plus a sign-out action.
// There is no verbose toggle: mobile.SetVerbose is wasm-only.
func newSettings(s *screen) fyne.CanvasObject {
	loggedIn := "No"
	if s.core.LoggedIn() {
		loggedIn = "Yes"
	}

	account := card(container.NewVBox(
		sectionTitle("Account"),
		kv("Base URL", "public cloud (default)"),
		kv("Signed in", loggedIn),
		caption("Device ID"),
		newCopyable(s.core.DeviceID()),
	))

	logout := dangerButton("Sign out", theme.LogoutIcon(), func() {
		confirmLogout(s.win, func() {
			if err := s.core.Logout(); err != nil {
				s.fail(err)
				return
			}
			s.toLogin()
		})
	})

	sections := []fyne.CanvasObject{
		pageHeader("Settings", "Session and account"),
		account,
	}
	// Only builds that carry an analytics key (official releases) show the opt-out toggle;
	// source/dev builds send nothing, so there is nothing to toggle.
	if s.core.AnalyticsAvailable() {
		sections = append(sections, newAnalyticsCard(s))
	}
	sections = append(sections,
		card(container.NewVBox(sectionTitle("Session"), container.NewHBox(logout))),
	)

	return container.NewVScroll(container.NewVBox(sections...))
}

// newAnalyticsCard renders the anonymous-usage opt-out toggle plus a plain-language note
// of exactly what is (and is not) sent.
func newAnalyticsCard(s *screen) fyne.CanvasObject {
	toggle := widget.NewCheck("Share anonymous usage analytics", func(on bool) {
		s.core.SetAnalyticsEnabled(on)
	})
	toggle.SetChecked(s.core.AnalyticsEnabled())
	return card(container.NewVBox(
		sectionTitle("Privacy"),
		toggle,
		captionWrap("Sends anonymous usage events (app opens, unlocks, credential changes) "+
			"with a random id, app version and OS — and NEVER your account, tokens, locks, or "+
			"codes. It helps prioritise what to build. Turn it off any time."),
	))
}

// newCopyable renders a value in a read-only, selectable entry.
func newCopyable(v string) fyne.CanvasObject {
	e := widget.NewEntry()
	e.SetText(v)
	e.Disable()
	return e
}
