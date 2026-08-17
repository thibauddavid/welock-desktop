package ui

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/thibauddavid/welock-desktop/internal/app"
)

// newBluetoothTab builds the direct-radio tab. The screen keeps only its primary
// affordances visible — a Scan/connection card, a big Unlock button, and quiet
// status/battery reads — while the less-frequent "add credential" mints live behind a
// submenu of focused modals. When the build has no radio it shows an explanatory notice.
func newBluetoothTab(dc *deviceCtx) fyne.CanvasObject {
	if !dc.core.BleAvailable() {
		return container.NewCenter(container.NewGridWrap(fyne.NewSize(420, 200), card(container.NewVBox(
			sectionTitle("Bluetooth"),
			h2("Radio unavailable in this build"),
			caption("Rebuild with -tags tinygobt to enable native BLE scanning and unlock."),
		))))
	}

	// selectedAddr is the address of the lock the user picked from a scan; the empty
	// string means "nothing selected yet" and gates every direct action.
	var selectedAddr string

	// The connection card shows which lock we'll talk to (or a hint to scan first).
	connLbl := widget.NewLabel("No lock selected — scan and pick one to connect.")
	connLbl.Wrapping = fyne.TextWrapWord

	// A single shared progress/status affordance, reused by every async action and by
	// the credential mints (which expect the busy(on, msg) closure below).
	progress := widget.NewProgressBarInfinite()
	progress.Hide()
	statusLbl := widget.NewLabel("")
	statusLbl.Wrapping = fyne.TextWrapWord

	busy := func(on bool, msg string) {
		if on {
			progress.Show()
		} else {
			progress.Hide()
		}
		if msg != "" {
			statusLbl.SetText(msg)
		}
	}

	// addr resolves the selected lock, toasting a hint when none is chosen. Shared with
	// the credential-mint flows so they gate on the same selection.
	addr := func() (string, bool) {
		if selectedAddr == "" {
			dc.toast("Bluetooth", "Scan and select a lock first.")
			return "", false
		}
		return selectedAddr, true
	}

	// Unlock is the tab's primary action — disabled until a device is selected.
	var unlockBtn *widget.Button
	unlockBtn = primaryButton("Unlock", theme.ConfirmIcon(), func() {
		a, ok := addr()
		if !ok {
			return
		}
		busy(true, "Unlocking…")
		runAsync(dc.win,
			func() (struct{}, error) {
				return struct{}{}, dc.core.BleUnlock(context.Background(), a, dc.num(), dc.name())
			},
			func(struct{}) {
				busy(false, "Unlocked.")
				dc.toast("Bluetooth", "Unlock command sent.")
				dc.core.Track("unlock", map[string]any{"transport": "ble"})
			},
			func(err error) { busy(false, ""); dc.fail(err) },
		)
	})
	unlockBtn.Disable()

	// selectDevice records the pick and reflects it in the connection card.
	selectDevice := func(d app.BleDevice) {
		selectedAddr = d.Address
		connLbl.SetText(fmt.Sprintf("Connected to %s  (%s)", orDash(d.Name), d.Address))
		unlockBtn.Enable()
		busy(false, "")
	}

	scanBtn := subtleButton("Scan", theme.SearchIcon(), func() {
		busy(true, "Scanning…")
		runAsync(dc.win,
			func() ([]app.BleDevice, error) { return dc.core.BleScan(context.Background()) },
			func(found []app.BleDevice) {
				busy(false, fmt.Sprintf("Found %d device(s).", len(found)))
				openScanResults(dc, found, selectDevice)
			},
			func(err error) { busy(false, ""); dc.fail(err) },
		)
	})

	// Read actions surface the lock's live state without any cloud round-trip.
	statusBtn := subtleButton("Status", theme.InfoIcon(), func() {
		a, ok := addr()
		if !ok {
			return
		}
		busy(true, "Reading status…")
		runAsync(dc.win,
			func() (*app.BleStatus, error) { return dc.core.BleReadStatus(context.Background(), a) },
			func(st *app.BleStatus) {
				busy(false, "")
				openView(dc.win, "Lock status", card(container.NewVBox(
					kv("Random factor", fmt.Sprintf("%d", st.RandomFactor)),
					kv("Battery", fmt.Sprintf("%d%%", st.Battery)),
				)))
			},
			func(err error) { busy(false, ""); dc.fail(err) },
		)
	})

	batteryBtn := subtleButton("Battery", theme.MediaRecordIcon(), func() {
		a, ok := addr()
		if !ok {
			return
		}
		busy(true, "Reading battery…")
		runAsync(dc.win,
			func() (int, error) { return dc.core.BleReadBattery(context.Background(), a) },
			func(pct int) { busy(false, ""); dc.toast("Battery", fmt.Sprintf("Battery level: %d%%", pct)) },
			func(err error) { busy(false, ""); dc.fail(err) },
		)
	})

	// The credential mints (PIN / card / fingerprint) are secondary, so they collapse
	// into a single "Add credential ▾" submenu of focused modals in the tab header.
	credBtn := newBleCredentials(dc, addr, busy)

	body := container.NewVBox(
		pageHeader("Bluetooth", "Direct radio — scan, read and unlock", credBtn),
		section("Connection", container.NewVBox(
			connLbl,
			container.NewHBox(scanBtn),
			progress,
			statusLbl,
		)),
		section("Unlock", unlockBtn),
		section("Read", container.NewHBox(statusBtn, batteryBtn)),
	)
	return container.NewVScroll(container.NewPadded(body))
}

// openScanResults shows the scanned peripherals in a roomy modal; tapping a row records
// the selection (via onPick) and updates the connection card. Picking leaves the modal
// open so the user can compare RSSI, then dismiss with Close.
func openScanResults(dc *deviceCtx, found []app.BleDevice, onPick func(app.BleDevice)) {
	if len(found) == 0 {
		openView(dc.win, "Nearby locks",
			container.NewGridWrap(fyne.NewSize(420, 200),
				emptyState(theme.SearchIcon(), "No locks found nearby — move closer and scan again.")))
		return
	}

	list := widget.NewList(
		func() int { return len(found) },
		func() fyne.CanvasObject { return widget.NewLabel("device") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			d := found[id]
			o.(*widget.Label).SetText(fmt.Sprintf("%s   (%s)  %ddBm", orDash(d.Name), d.Address, d.RSSI))
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(found) {
			return
		}
		onPick(found[id])
	}
	openView(dc.win, "Nearby locks", container.NewGridWrap(fyne.NewSize(520, 420), list))
}

// newBleCredentials builds the "Add credential ▾" submenu button: PIN, access-card and
// fingerprint enrolment, each opening a focused modal (openForm) instead of a flat
// stacked form. Every item is gated on the device's BLE capability
// (CanBleAddPin / CanBleAddCard / CanBleAddFingerprint; unknown ⇒ enabled, `?? true`) and
// driven by the read-reply → mint → write → report glue in Core. addr resolves the
// selected scanned device; busy toggles the shared progress/status affordance.
func newBleCredentials(dc *deviceCtx, addr func() (string, bool), busy func(on bool, msg string)) *widget.Button {
	// add PIN. Bluetooth is the ONLY path that associates a credential with a user (the
	// remote/gateway path can only label a time-limited code) — so every BLE form carries a
	// user picker and passes it through to the mint.
	openPinForm := func() {
		pin := widget.NewEntry()
		pin.SetPlaceHolder("PIN")
		pinPresets := presetSelect(dc.core, func() {})
		user := dc.userSelect()
		openForm(dc.win, "Add PIN over BLE",
			[]*widget.FormItem{
				widget.NewFormItem("New PIN", pin),
				widget.NewFormItem("Validity", pinPresets.sel),
				widget.NewFormItem("User", user),
			},
			func() {
				a, ok := addr()
				if !ok {
					return
				}
				if msg := dc.core.ValidatePin(dc.item.DeviceModel, pin.Text); msg != "" {
					dc.toast("Invalid PIN", msg)
					return
				}
				start, end := pinPresets.window()
				u := strings.TrimSpace(user.Text)
				busy(true, "Adding PIN over BLE…")
				runAsync(dc.win,
					func() (struct{}, error) {
						return struct{}{}, dc.core.BleSetPassword(context.Background(), a, dc.num(), dc.name(), pin.Text, start, end, 0, u, "")
					},
					func(struct{}) {
						busy(false, "PIN added.")
						dc.toast("Bluetooth", "PIN added over BLE.")
						dc.core.Track("credential_added", map[string]any{"kind": "pin", "via": "ble"})
					},
					func(err error) { busy(false, ""); dc.fail(err) },
				)
			})
	}

	// add card
	openCardForm := func() {
		cardNo := widget.NewEntry()
		cardNo.SetPlaceHolder("card number")
		cardPresets := presetSelect(dc.core, func() {})
		user := dc.userSelect()
		openForm(dc.win, "Add access card over BLE",
			[]*widget.FormItem{
				widget.NewFormItem("Card number", cardNo),
				widget.NewFormItem("Validity", cardPresets.sel),
				widget.NewFormItem("User", user),
			},
			func() {
				a, ok := addr()
				if !ok {
					return
				}
				start, end := cardPresets.window()
				u := strings.TrimSpace(user.Text)
				busy(true, "Adding card over BLE…")
				runAsync(dc.win,
					func() (struct{}, error) {
						return struct{}{}, dc.core.BleAddCard(context.Background(), a, dc.num(), dc.name(), cardNo.Text, start, end, 0, u)
					},
					func(struct{}) {
						busy(false, "Card added.")
						dc.toast("Bluetooth", "Card added over BLE.")
						dc.core.Track("credential_added", map[string]any{"kind": "card", "via": "ble"})
					},
					func(err error) { busy(false, ""); dc.fail(err) },
				)
			})
	}

	// add fingerprint (captured at the lock's sensor after the command is written)
	openFpForm := func() {
		info := widget.NewLabel("Start enrolment, then present the finger at the lock's sensor.")
		info.Wrapping = fyne.TextWrapWord
		user := dc.userSelect()
		openForm(dc.win, "Add fingerprint over BLE",
			[]*widget.FormItem{
				widget.NewFormItem("User", user),
				widget.NewFormItem("", info),
			},
			func() {
				a, ok := addr()
				if !ok {
					return
				}
				u := strings.TrimSpace(user.Text)
				busy(true, "Starting fingerprint enrolment…")
				runAsync(dc.win,
					func() (struct{}, error) {
						return struct{}{}, dc.core.BleAddFingerprint(context.Background(), a, dc.num(), dc.name(), u)
					},
					func(struct{}) {
						busy(false, "Follow the lock's prompts to capture the fingerprint.")
						dc.toast("Bluetooth", "Fingerprint enrolment started — present the finger at the lock.")
						dc.core.Track("credential_added", map[string]any{"kind": "fingerprint", "via": "ble"})
					},
					func(err error) { busy(false, ""); dc.fail(err) },
				)
			})
	}

	// Build the submenu from the core credential enum, keeping only BLE-addable types
	// that have a bespoke mint form here (Password → PIN, Card → card, Fingerprint →
	// fingerprint). The Name→form mapping stays local; the SET is now derived from bff.
	forms := map[string]func(){
		"Password":    openPinForm,
		"Card":        openCardForm,
		"Fingerprint": openFpForm,
	}
	type gatedItem struct {
		item *fyne.MenuItem
		name string
	}
	var items []*fyne.MenuItem
	var gated []gatedItem
	for _, ct := range dc.core.CredentialTypes() {
		if !ct.BleAddable {
			continue
		}
		form, ok := forms[ct.Name]
		if !ok {
			continue // no bespoke form in this context
		}
		item := fyne.NewMenuItem(ct.Label, form)
		items = append(items, item)
		gated = append(gated, gatedItem{item: item, name: ct.Name})
	}

	// Gate each item on the device's BLE capability (unknown ⇒ enabled). The submenu is
	// rebuilt from these item pointers on every open, so toggling Disabled here takes
	// effect the next time the user opens the menu.
	dc.onCaps(func() {
		c := dc.caps()
		for _, g := range gated {
			disabled := false
			if c != nil {
				switch g.name {
				case "Password":
					disabled = !c.CanBleAddPin
				case "Card":
					disabled = !c.CanBleAddCard
				case "Fingerprint":
					disabled = !c.CanBleAddFingerprint
				}
			}
			g.item.Disabled = disabled
		}
	})

	return menuButton("Add credential", theme.ContentAddIcon(), widget.MediumImportance, items...)
}
