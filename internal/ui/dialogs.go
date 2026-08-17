package ui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// formDialog wraps embedded form content on a custom dialog with a primary confirm
// action plus a Cancel dismiss, so every add-flow reads like the rest of the app.
// content is the styled body (field()/caption blocks); onConfirm runs the exact same
// work the old ShowForm callbacks did, after the dialog is dismissed.
func formDialog(win fyne.Window, title, confirm string, icon fyne.Resource, content *fyne.Container, onConfirm func()) *dialog.CustomDialog {
	var d *dialog.CustomDialog
	btn := primaryButton(confirm, icon, func() {
		d.Hide()
		onConfirm()
	})
	content.Add(container.NewPadded(btn))
	d = dialog.NewCustom(title, "Cancel", content, win)

	min := d.MinSize()
	w := float32(460)
	if min.Width > w {
		w = min.Width
	}
	d.Resize(fyne.NewSize(w, min.Height))
	return d
}

// newAddChooser is a focused hub for everything the "+" can add: two drill-in rows
// for the lock/gateway bind forms, one for redeeming an activation code, plus a quiet
// "View my codes" affordance. Each row opens its own modal — the chooser itself stays a
// short, scannable list rather than a wall of stacked forms.
func newAddChooser(s *screen, onDone func()) {
	var d *dialog.CustomDialog
	pick := func(fn func()) func() { return func() { d.Hide(); fn() } }

	content := container.NewVBox(
		navRow(theme.HomeIcon(), "Add lock", "Bind a lock from its QR payload",
			pick(func() { newAddDevice(s, onDone) })),
		navRow(theme.ComputerIcon(), "Add gateway", "Register a gateway by QR or name/MAC",
			pick(func() { newAddGateway(s, onDone) })),
		navRow(theme.ContentAddIcon(), "Redeem activation code", "Bind a lock using an activation code",
			pick(func() { newRedeemActivation(s, onDone) })),
		navRow(theme.ListIcon(), "View my codes", "See the activation codes on your account",
			pick(func() { showActivationCodes(s) })),
	)
	d = dialog.NewCustom("Add", "Cancel", content, s.win)
	d.Show()
}

// newRedeemActivation binds a lock from a one-shot activation code. The device's timezone
// offset is passed so the core can align the lock clock.
func newRedeemActivation(s *screen, onDone func()) {
	code := widget.NewEntry()
	code.SetPlaceHolder("activation code")

	openForm(s.win, "Redeem activation code",
		[]*widget.FormItem{widget.NewFormItem("Activation code", code)},
		func() {
			c := strings.TrimSpace(code.Text)
			if c == "" {
				s.toast("Activation", "An activation code is required.")
				return
			}
			_, tzOff := time.Now().Zone()
			runAsync(s.win,
				func() (string, error) { return s.core.ActivationBind(context.Background(), c, tzOff) },
				func(string) { s.toast("Activation", "Code redeemed."); onDone() },
				s.fail,
			)
		})
}

// showActivationCodes fetches the account's activation codes and shows the raw reply in a
// roomy scrollable modal. The payload is raw JSON, so it is rendered defensively as text.
func showActivationCodes(s *screen) {
	runAsync(s.win,
		func() (string, error) { return s.core.ActivationList(context.Background(), 1) },
		func(js string) {
			body := widget.NewLabel(orDash(strings.TrimSpace(js)))
			body.Wrapping = fyne.TextWrapBreak
			openView(s.win, "My activation codes", container.NewVScroll(body))
		},
		s.fail,
	)
}

// newAddDevice binds a lock from a pasted QR payload plus manual type/model.
func newAddDevice(s *screen, onDone func()) {
	qr := widget.NewMultiLineEntry()
	qr.SetPlaceHolder("QR payload string")
	qr.SetMinRowsVisible(3)
	number := widget.NewEntry()
	number.SetPlaceHolder("device number")
	dType := widget.NewEntry()
	dType.SetPlaceHolder("deviceType")
	dModel := widget.NewEntry()
	dModel.SetPlaceHolder("deviceModel")

	openForm(s.win, "Add lock",
		[]*widget.FormItem{
			widget.NewFormItem("QR payload", qr),
			widget.NewFormItem("Device number", number),
			widget.NewFormItem("Type", dType),
			widget.NewFormItem("Model", dModel),
		},
		func() {
			runAsync(s.win,
				func() (string, error) {
					return s.core.BindDevice(context.Background(), strings.TrimSpace(number.Text),
						strings.TrimSpace(qr.Text), strings.TrimSpace(dType.Text),
						strings.TrimSpace(dModel.Text), 0, 0, "")
				},
				func(string) { s.toast("Device", "Lock added."); onDone() },
				s.fail,
			)
		})
}

// newAddGateway registers a gateway from a QR payload or a name/MAC pair.
func newAddGateway(s *screen, onDone func()) {
	qr := widget.NewEntry()
	qr.SetPlaceHolder("QR payload (optional)")
	name := widget.NewEntry()
	name.SetPlaceHolder("name")
	mac := widget.NewEntry()
	mac.SetPlaceHolder("MAC")
	remark := widget.NewEntry()
	remark.SetPlaceHolder("remark")

	openForm(s.win, "Add gateway",
		[]*widget.FormItem{
			widget.NewFormItem("QR payload", qr),
			widget.NewFormItem("Name", name),
			widget.NewFormItem("MAC", mac),
			widget.NewFormItem("Remark", remark),
		},
		func() {
			runAsync(s.win,
				func() (string, error) {
					if q := strings.TrimSpace(qr.Text); q != "" {
						return s.core.AddGatewayQr(context.Background(), q)
					}
					return s.core.AddGateway(context.Background(), strings.TrimSpace(name.Text),
						strings.TrimSpace(mac.Text), strings.TrimSpace(remark.Text))
				},
				func(string) { s.toast("Gateway", "Gateway added."); onDone() },
				s.fail,
			)
		})
}

// newAddTempPassword creates a temporary password and reveals the generated PIN.
func newAddTempPassword(dc *deviceCtx, onDone func()) {
	presets := presetSelect(dc.core, nil)
	remark := widget.NewEntry()
	remark.SetPlaceHolder("remark")
	typ := widget.NewSelect([]string{"Continuity (0)", "One-time (1)"}, nil)
	typ.SetSelectedIndex(0)

	content := container.NewVBox(
		caption("Generates a time-limited PIN for this lock."),
		field("Validity", presets.sel),
		field("Type", typ),
		field("Remark", remark),
	)

	d := formDialog(dc.win, "Add temporary password", "Create", theme.ContentAddIcon(), content, func() {
		start, end := presets.window()
		t := 0
		if typ.SelectedIndex() == 1 {
			t = 1
		}
		// Offline codes are keyed to the lock's clock — sync it as part of creating one so
		// the fresh code lands inside its window on the lock.
		dc.syncLockClock()
		runAsync(dc.win,
			func() (string, error) {
				return dc.core.AddTempPassword(context.Background(), dc.num(), dc.name(), start, end, remark.Text, t)
			},
			func(pin string) {
				dialog.ShowInformation("Temporary password", "PIN: "+pin, dc.win)
				onDone()
			},
			dc.fail,
		)
	})
	d.Show()
}

// newAddPermission shares the lock with another account.
func newAddPermission(dc *deviceCtx, onDone func()) {
	account := widget.NewEntry()
	account.SetPlaceHolder("account (email/phone)")
	role := widget.NewSelect([]string{"User (1)", "Admin (2)"}, nil)
	role.SetSelectedIndex(0)
	presets := presetSelect(dc.core, nil)
	unlocks := widget.NewEntry()
	unlocks.SetPlaceHolder("unlock count (0 = unlimited)")
	unlocks.SetText("0")
	remark := widget.NewEntry()
	remark.SetPlaceHolder("remark")

	content := container.NewVBox(
		caption("Share this lock with another WeLock account."),
		field("Account", account),
		field("Role", role),
		field("Validity", presets.sel),
		field("Unlocks", unlocks),
		caption("0 means unlimited unlocks."),
		field("Remark", remark),
	)

	d := formDialog(dc.win, "Share access", "Share", theme.ContentAddIcon(), content, func() {
		if strings.TrimSpace(account.Text) == "" {
			dc.toast("Share", "An account is required.")
			return
		}
		roleID := 1
		if role.SelectedIndex() == 1 {
			roleID = 2
		}
		begin, end := presets.window()
		n, _ := strconv.Atoi(strings.TrimSpace(unlocks.Text))
		runAsync(dc.win,
			func() (string, error) {
				return dc.core.AddPermission(context.Background(), dc.num(), account.Text, roleID, begin, end, n, remark.Text)
			},
			func(string) { dc.toast("Share", "Access shared."); onDone() },
			dc.fail,
		)
	})
	d.Show()
}
