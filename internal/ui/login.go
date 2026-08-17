package ui

import (
	"context"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/thibauddavid/welock-desktop/internal/app"
)

// NewLogin builds the sign-in screen: a branded gradient hero on the left and a
// sign-in card on the right offering three flows — WhatsApp, account/password, and
// an access token — all sharing one onLogin callback (invoked on the main goroutine
// once tokens are held).
func NewLogin(a fyne.App, win fyne.Window, core *app.Core, onLogin func()) fyne.CanvasObject {
	s := &screen{app: a, win: win, core: core}

	// Every login-success/exit path must tear down the WhatsApp poll ticker so it does
	// not keep hitting the network for the life of the process.
	var stopWhatsApp func()
	onExit := func() {
		if stopWhatsApp != nil {
			stopWhatsApp()
		}
		onLogin()
	}

	whatsAppContent, stop := s.whatsAppLogin(onExit)
	stopWhatsApp = stop

	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("WhatsApp", theme.MailComposeIcon(), whatsAppContent),
		container.NewTabItemWithIcon("Password", theme.LoginIcon(), s.passwordLogin(onExit)),
		container.NewTabItemWithIcon("Token", theme.ContentPasteIcon(), s.tokenLogin(onExit)),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	form := container.NewVBox(
		h1("Welcome back"),
		caption("Sign in to manage your locks"),
		widget.NewSeparator(),
		tabs,
	)
	right := container.NewCenter(container.NewGridWrap(fyne.NewSize(430, 480), card(form)))

	hero := heroPane("Your locks, on your desktop")
	return container.NewGridWithColumns(2, hero, right)
}

// whatsAppLogin builds the WhatsApp challenge flow. It returns its content plus a stop
// closure that cancels any in-flight poll ticker; NewLogin calls stop on every login
// exit path so the goroutine never outlives the login screen.
func (s *screen) whatsAppLogin(onLogin func()) (fyne.CanvasObject, func()) {
	status := widget.NewLabel("Start a WhatsApp login, send the prefilled message, then wait here.")
	status.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	var startBtn, cancelBtn *widget.Button
	var cancel context.CancelFunc

	stop := func() {
		if cancel != nil {
			cancel()
			cancel = nil
		}
		progress.Hide()
		cancelBtn.Hide()
		startBtn.Enable()
		startBtn.SetText("Start WhatsApp login")
	}

	startBtn = primaryButton("Start WhatsApp login", theme.MailComposeIcon(), nil)
	startBtn.OnTapped = func() {
		startBtn.Disable()
		cancelBtn.Show()
		progress.Show()
		status.SetText("Requesting login challenge…")
		runAsync(s.win,
			func() (*app.WhatsAppLogin, error) { return s.core.StartWhatsAppLogin(context.Background()) },
			func(wl *app.WhatsAppLogin) {
				if u, err := url.Parse(wl.URL); err == nil && wl.URL != "" {
					_ = s.app.OpenURL(u)
				}
				status.SetText("Sent. Complete the opened WhatsApp message, then keep this window open…")
				ctx, c := context.WithCancel(context.Background())
				cancel = c
				s.pollWhatsApp(ctx, wl.Code, onLogin, status, stop)
			},
			func(err error) {
				stop()
				status.SetText("Failed to start: " + err.Error())
				s.fail(err)
			},
		)
	}

	cancelBtn = subtleButton("Cancel", theme.CancelIcon(), func() { stop(); status.SetText("Cancelled.") })
	cancelBtn.Hide()

	content := container.NewVBox(
		status,
		progress,
		startBtn,
		cancelBtn,
	)
	return content, stop
}

// pollWhatsApp polls PollWhatsAppLogin on a ticker until tokens, error, or ctx cancel.
func (s *screen) pollWhatsApp(ctx context.Context, code string, onLogin func(), status *widget.Label, stop func()) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				toks, err := s.core.PollWhatsAppLogin(ctx, code)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					fyne.Do(func() {
						stop()
						status.SetText("Login failed: " + err.Error())
					})
					return
				}
				if toks != nil {
					fyne.Do(func() {
						stop()
						onLogin()
					})
					return
				}
				// still pending — keep polling
			}
		}
	}()
}

// passwordLogin builds the account/password flow.
func (s *screen) passwordLogin(onLogin func()) fyne.CanvasObject {
	account := widget.NewEntry()
	account.SetPlaceHolder("email or phone")
	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("password")

	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	submit := primaryButton("Sign in", theme.LoginIcon(), nil)
	submit.OnTapped = func() {
		if strings.TrimSpace(account.Text) == "" || password.Text == "" {
			dialog.ShowInformation("Missing details", "Enter an account and password.", s.win)
			return
		}
		submit.Disable()
		progress.Show()
		runAsync(s.win,
			func() (*app.Tokens, error) {
				return s.core.Login(context.Background(), account.Text, password.Text)
			},
			func(_ *app.Tokens) {
				progress.Hide()
				submit.Enable()
				onLogin()
			},
			func(err error) {
				progress.Hide()
				submit.Enable()
				s.fail(err)
			},
		)
	}

	return container.NewVBox(
		field("Account", account),
		field("Password", password),
		progress,
		container.NewPadded(submit),
	)
}

// tokenLogin builds the "sign in with an existing access token" flow.
func (s *screen) tokenLogin(onLogin func()) fyne.CanvasObject {
	access := widget.NewMultiLineEntry()
	access.SetPlaceHolder("Paste your access token")
	access.Wrapping = fyne.TextWrapBreak
	access.SetMinRowsVisible(3)

	refresh := widget.NewEntry()
	refresh.SetPlaceHolder("Refresh token (optional)")

	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	submit := primaryButton("Sign in with token", theme.ConfirmIcon(), nil)
	submit.OnTapped = func() {
		if strings.TrimSpace(access.Text) == "" {
			dialog.ShowInformation("Missing token", "Paste an access token.", s.win)
			return
		}
		submit.Disable()
		progress.Show()
		runAsync(s.win,
			func() (struct{}, error) {
				return struct{}{}, s.core.LoginWithToken(context.Background(), access.Text, refresh.Text)
			},
			func(struct{}) {
				progress.Hide()
				submit.Enable()
				onLogin()
			},
			func(err error) {
				progress.Hide()
				submit.Enable()
				s.fail(err)
			},
		)
	}

	return container.NewVBox(
		field("Access token", access),
		field("Refresh token", refresh),
		caption("The token is validated once, then stored locally like a normal sign-in."),
		progress,
		container.NewPadded(submit),
	)
}
