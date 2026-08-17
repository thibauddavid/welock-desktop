package ui

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/thibauddavid/welock-desktop/internal/app"
)

// screen is the shared context threaded through every UI surface. It carries the
// backend Core, the host window/app (for dialogs + OpenURL) and a toLogin callback
// used both by the explicit logout action and by the automatic auth-failure path.
type screen struct {
	app     fyne.App
	win     fyne.Window
	core    *app.Core
	toLogin func()
	// refresh reloads the sidebar lists; home resets the detail pane to the placeholder.
	// Both are set by the main window (nil on the login screen) so detail actions
	// (rename/transfer/delete) can keep the list in sync.
	refresh func()
	home    func()
}

// runAsync runs work() on a goroutine and delivers the result back on the Fyne
// main goroutine via fyne.Do. Widgets are NEVER touched off the main goroutine.
// A nil onErr defaults to a modal error dialog.
func runAsync[T any](win fyne.Window, work func() (T, error), onOK func(T), onErr func(error)) {
	go func() {
		res, err := work()
		fyne.Do(func() {
			if err != nil {
				if onErr != nil {
					onErr(err)
				} else {
					dialog.ShowError(err, win)
				}
				return
			}
			if onOK != nil {
				onOK(res)
			}
		})
	}()
}

// userSelect builds an editable "who is this credential for?" combobox: a SelectEntry seeded
// with the lock's existing owners (from the core's bff.CredentialOwners) that also accepts a
// new name. Options load asynchronously; it is a usable free-text field meanwhile.
func (dc *deviceCtx) userSelect() *widget.SelectEntry {
	e := widget.NewSelectEntry(nil)
	e.PlaceHolder = "pick an existing user or type a new name"
	runAsync(dc.win,
		func() ([]string, error) { return dc.core.MemberOwners(context.Background(), dc.num(), dc.name()) },
		func(owners []string) { e.SetOptions(owners) },
		func(error) {},
	)
	return e
}

// fileMember records a credential's label in the cloud member list (fire-and-forget) so a
// gateway-programmed card groups under its chosen label. Used only where the gateway op
// carries no label field (UfunAddCard); the PIN path relies on UfunSetPassword's own
// `remark`, so it does NOT file a mirror (that would leak the PIN into the number/ID slot).
// Gateway credentials are labeled, not user-associated — a real user tie is Bluetooth-only.
func (dc *deviceCtx) fileMember(typeName, label, number string) {
	if label == "" {
		return
	}
	runAsync(dc.win,
		func() (string, error) {
			return dc.core.AddMember(context.Background(), dc.num(), dc.name(), typeName, label, number)
		},
		func(string) {},
		func(error) {},
	)
}

// fail is the standard error sink: a single-session token expiry (IsAuthError)
// bounces the user back to the login screen; anything else is shown as a dialog.
func (s *screen) fail(err error) {
	if err == nil {
		return
	}
	if app.IsAuthError(err) {
		dialog.ShowError(fmt.Errorf("session expired — please sign in again"), s.win)
		if s.toLogin != nil {
			s.toLogin()
		}
		return
	}
	dialog.ShowError(err, s.win)
}

// toast shows a transient information dialog (used for command outcomes / confirmations).
func (s *screen) toast(title, msg string) {
	dialog.ShowInformation(title, msg, s.win)
}

// --- battery meter --------------------------------------------------------

// batteryMeter is a compact color-coded battery chip with a percentage label.
type batteryMeter struct {
	obj   *fyne.Container
	chip  *canvas.Rectangle
	label *widget.Label
}

// newBatteryMeter builds a battery meter in its unknown state.
func newBatteryMeter() *batteryMeter {
	chip := canvas.NewRectangle(batteryGreen)
	chip.SetMinSize(fyne.NewSize(14, 14))
	chip.CornerRadius = 3
	label := widget.NewLabel("—")
	m := &batteryMeter{
		chip:  chip,
		label: label,
	}
	m.obj = container.NewHBox(container.NewCenter(chip), label)
	m.Set(0, false)
	return m
}

// Object returns the meter's canvas object for placement.
func (m *batteryMeter) Object() fyne.CanvasObject { return m.obj }

// Set updates the meter; known=false renders the unknown ("—") state.
func (m *batteryMeter) Set(pct int, known bool) {
	if !known {
		m.label.SetText("—")
		m.chip.FillColor = theme.Color(theme.ColorNameInputBackground)
		canvas.Refresh(m.chip)
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	m.label.SetText(fmt.Sprintf("%d%%", pct))
	switch {
	case pct <= 15:
		m.chip.FillColor = batteryRed
	case pct <= 35:
		m.chip.FillColor = batteryAmber
	default:
		m.chip.FillColor = batteryGreen
	}
	canvas.Refresh(m.chip)
}

// --- command status polling ----------------------------------------------

// pollCommand polls CommandStatus(id) to a terminal state (mirrors the web's
// pollCommandStatus: 1=success, 2/8=failed, else pending; timeout ⇒ unknown).
// onDone runs on the Fyne main goroutine.
func (s *screen) pollCommand(id string, onDone func(outcome string)) {
	go func() {
		ctx := context.Background()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			cs, err := s.core.CommandStatus(ctx, id)
			if err == nil && cs != nil {
				switch statusInt(cs["status"]) {
				case 1:
					fyne.Do(func() { onDone("success") })
					return
				case 2, 8:
					fyne.Do(func() { onDone("failed") })
					return
				}
			}
			time.Sleep(1500 * time.Millisecond)
		}
		fyne.Do(func() { onDone("unknown") })
	}()
}

// statusInt coerces a loose JSON value (float64 | string | int) into an int, or -1.
func statusInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		var i int
		if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
			return i
		}
	}
	return -1
}

// looseInt reads an int-ish value out of a loose map (float64|string|int), ok=false if absent.
func looseInt(m map[string]any, keys ...string) (int, bool) {
	for _, k := range keys {
		if v, present := m[k]; present {
			if i := statusInt(v); i != -1 || fmt.Sprint(v) == "0" || fmt.Sprint(v) == "-1" {
				return i, true
			}
		}
	}
	return 0, false
}

// looseString reads a string-ish value out of a loose map, "" if absent.
func looseString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}

// setEnabled enables or disables a button from a bool (capability gating helper).
func setEnabled(b *widget.Button, on bool) {
	if on {
		b.Enable()
	} else {
		b.Disable()
	}
}

// section groups content under an uppercase eyebrow title on a card surface — the
// standard way every tab lays out a block of related controls.
func section(title string, content fyne.CanvasObject) fyne.CanvasObject {
	return card(container.NewVBox(sectionTitle(title), content))
}

// describedAction lays a fixed-width action button beside a muted one-line description,
// so every action explains itself (the desktop stand-in for the web's button tooltips).
func describedAction(btn *widget.Button, desc string) fyne.CanvasObject {
	btn.Alignment = widget.ButtonAlignLeading // left-align icon+label so all actions line up
	// Fixed-width button column; the description fills the rest and word-wraps, so the row
	// grows to fit multiple lines instead of the text overflowing/overlapping.
	left := container.NewGridWrap(fyne.NewSize(190, 36), btn)
	d := widget.NewLabel(desc)
	d.Wrapping = fyne.TextWrapWord
	d.Importance = widget.LowImportance
	return container.NewBorder(nil, nil, left, nil, d)
}

// fmtUnix renders a unix-second timestamp; 0 renders as "no expiry".
func fmtUnix(ts int64) string {
	if ts == 0 {
		return "no expiry"
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04")
}

// placeholder renders a centered empty-state (icon + muted message) for a blank pane.
func placeholder(msg string) fyne.CanvasObject {
	return emptyState(theme.HomeIcon(), msg)
}

// orDash returns s, or "—" when empty.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// yesNo renders a bool as Yes/No.
func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// confirm shows a yes/no destructive confirmation, invoking onYes on accept.
func confirm(win fyne.Window, message string, onYes func()) {
	dialog.ShowConfirm("Please confirm", message, func(ok bool) {
		if ok {
			onYes()
		}
	}, win)
}

// confirmLogout confirms signing out.
func confirmLogout(win fyne.Window, onYes func()) {
	dialog.ShowConfirm("Sign out", "Sign out of this account?", func(ok bool) {
		if ok {
			onYes()
		}
	}, win)
}

// promptText shows a single-field text prompt and calls onOK with the trimmed value.
func promptText(win fyne.Window, title, label, initial string, onOK func(string)) {
	entry := widget.NewEntry()
	entry.SetText(initial)
	dialog.ShowForm(title, "OK", "Cancel",
		[]*widget.FormItem{widget.NewFormItem(label, entry)},
		func(ok bool) {
			if ok {
				onOK(entry.Text)
			}
		}, win)
}

// presetPicker is a validity-preset dropdown plus a window() resolver. It holds the core
// so window() resolves the selected preset via the helper (the rule lives there).
type presetPicker struct {
	sel     *widget.Select
	byLabel map[string]string
	core    *app.Core
}

// presetSelect builds a validity-preset picker (defaults to the first preset) from the
// core's canonical preset list. onChange is invoked (may be nil) on selection change.
func presetSelect(core *app.Core, onChange func()) *presetPicker {
	return presetSelectExcept(core, onChange)
}

// presetSelectExcept is presetSelect without the given preset keys — used to drop
// "permanent" for gateway credentials, which the lock only accepts time-limited.
func presetSelectExcept(core *app.Core, onChange func(), exclude ...string) *presetPicker {
	skip := map[string]bool{}
	for _, k := range exclude {
		skip[k] = true
	}
	var labels []string
	byLabel := map[string]string{}
	for _, p := range core.ValidityPresets() {
		if skip[p.Key] {
			continue
		}
		labels = append(labels, p.Label)
		byLabel[p.Label] = p.Key
	}
	sel := widget.NewSelect(labels, func(string) {
		if onChange != nil {
			onChange()
		}
	})
	if len(labels) > 0 {
		sel.SetSelectedIndex(0)
	}
	return &presetPicker{sel: sel, byLabel: byLabel, core: core}
}

// window resolves the current preset selection into a [start,end] unix-second window.
func (p *presetPicker) window() (start, end int64) {
	return p.core.ValidityWindow(p.byLabel[p.sel.Selected], time.Now().Unix())
}
