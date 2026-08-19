package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/thibauddavid/welock-desktop/internal/app"
)

// newRemoteTab builds the gateway-relayed control tab. It keeps one prominent primary
// action — Unlock — visible, and pushes everything else behind quiet buttons and modals:
// status reads open a roomy result view, the relay trigger and credential management open
// focused forms/lists, so the screen reads as a short menu instead of a wall of cards.
func newRemoteTab(dc *deviceCtx) fyne.CanvasObject {
	statusLbl := widget.NewLabel("Ready.")
	statusLbl.Wrapping = fyne.TextWrapWord

	// --- (1) Unlock: the single primary call-to-action ---
	unlock := primaryButton("Unlock", theme.ConfirmIcon(), func() {
		dc.runCommand(statusLbl, "Remote unlock", func() (string, error) {
			return dc.core.RemoteUnlock(context.Background(), dc.num(), dc.name())
		}, func(outcome string) {
			if outcome == "success" {
				dc.core.Track("unlock", map[string]any{"transport": "gateway"})
			}
		})
	})
	unlockNote := caption("Remote unlock isn't available for this lock.")
	unlockNote.Hide()

	// Gate gateway-relayed unlock on the device's reported capability (unknown ⇒ shown,
	// mirroring the web's `?? true`); real capabilities disable it once DeviceInfo loads.
	dc.onCaps(func() {
		c := dc.caps()
		on := c == nil || c.SupportsGatewayUnlock
		setEnabled(unlock, on)
		if on {
			unlockNote.Hide()
		} else {
			unlockNote.Show()
		}
	})

	unlockCard := section("Unlock", container.NewVBox(
		caption("Send an open command to the lock through its gateway."),
		unlock,
		unlockNote,
	))

	// --- (2) Status: read-only lookups that open a roomy result view ---
	lockStatusBtn := subtleButton("Check lock status", theme.InfoIcon(), func() {
		runAsync(dc.win,
			func() (app.LockStatus, error) { return dc.core.LockStatus(context.Background(), dc.num(), dc.name()) },
			func(ls app.LockStatus) { openView(dc.win, "Lock status", looseView(ls)) },
			dc.fail,
		)
	})
	remoteBatBtn := subtleButton("Battery & signal", theme.MediaRecordIcon(), func() {
		runAsync(dc.win,
			func() (app.RemoteLockInfo, error) { return dc.core.RemoteLockInfo(context.Background(), dc.num()) },
			func(ri app.RemoteLockInfo) { openView(dc.win, "Battery & signal", looseView(ri)) },
			dc.fail,
		)
	})
	statusCard := section("Status", container.NewVBox(
		describedAction(lockStatusBtn, "Ask the gateway to read the lock's live state (read-only)."),
		describedAction(remoteBatBtn, "Read battery and signal via the gateway (read-only)."),
	))

	// --- (3) More: secondary actions, each behind a modal ---
	feibi := subtleButton("Feibi unlock", theme.ConfirmIcon(), func() {
		dc.runCommand(statusLbl, "Feibi unlock", func() (string, error) {
			return dc.core.FeibiLockOpen(context.Background(), dc.num(), dc.name())
		}, func(outcome string) {
			if outcome == "success" {
				dc.core.Track("unlock", map[string]any{"transport": "feibi"})
			}
		})
	})
	relay := subtleButton("Relay…", theme.MediaPlayIcon(), func() {
		dc.openRelayForm(statusLbl)
	})
	clock := subtleButton("Sync clock", theme.HistoryIcon(), func() {
		_, tzOff := time.Now().Zone()
		dc.runGatewayCommand("Sync clock", func() (string, error) {
			return dc.core.UfunSetLockTime(context.Background(), dc.num(), dc.name(), "", tzOff)
		}, nil)
	})
	creds := subtleButton("Credentials…", theme.AccountIcon(), func() {
		dc.showGatewayCredentials()
	})
	moreCard := section("More", container.NewVBox(
		describedAction(feibi, "For Feibi-bridged locks — if Remote unlock doesn't apply."),
		describedAction(relay, "Fire the gateway's relay / dry-contact output — not this lock."),
		describedAction(clock, "Set the lock's clock via the gateway."),
		describedAction(creds, "Add or remove keypad PINs and access cards."),
	))

	body := container.NewVBox(
		pageHeader("Remote control", "Gateway-relayed actions"),
		unlockCard,
		statusCard,
		moreCard,
	)

	return container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), statusLbl),
		nil, nil,
		container.NewVScroll(container.NewPadded(body)),
	)
}

// openRelayForm opens the relay-trigger modal: an optional gateway value and a delay. A
// blank gateway falls back to this device's number. The raw reply is surfaced in statusLbl.
func (dc *deviceCtx) openRelayForm(statusLbl *widget.Label) {
	gw := widget.NewEntry()
	gw.SetPlaceHolder("gateway value (blank = this device)")
	delay := widget.NewEntry()
	delay.SetPlaceHolder("seconds (0 = now)")
	delay.SetText("0")
	openForm(dc.win, "Trigger relay",
		[]*widget.FormItem{
			widget.NewFormItem("Gateway", gw),
			widget.NewFormItem("Delay (s)", delay),
		},
		func() {
			g := strings.TrimSpace(gw.Text)
			if g == "" {
				g = dc.num()
			}
			d, _ := strconv.Atoi(strings.TrimSpace(delay.Text))
			statusLbl.SetText("Triggering relay…")
			runAsync(dc.win,
				func() (string, error) {
					if d > 0 {
						return dc.core.TriggerSwitchDelay(context.Background(), g, d)
					}
					return dc.core.TriggerSwitch(context.Background(), g)
				},
				func(reply string) { statusLbl.SetText("Relay: " + orDash(reply)) },
				func(err error) { statusLbl.SetText("Relay failed"); dc.fail(err) },
			)
		})
}

// --- gateway credentials modal --------------------------------------------

// showGatewayCredentials opens the keypad-PIN & card manager: two labelled sections, each
// with its own list and Add button. Keypad PINs are the unified list (permanent + time-
// limited, badged); cards are the gateway-managed RFID cards. Add is gated on capabilities.
func (dc *deviceCtx) showGatewayCredentials() {
	c := dc.caps()

	pinBox := container.NewVBox()
	reloadPins := dc.loadKeypadPins(pinBox)
	pinAdd := primaryButton("Add PIN", theme.ContentAddIcon(), func() {
		dc.addGatewayPin(pinBox, reloadPins)
	})
	if c != nil && !c.CanGatewayAddPin {
		pinAdd.Disable()
	}

	cardBox := container.NewVBox()
	reloadCards := dc.loadGatewayCards(cardBox)
	cardAdd := primaryButton("Add card", theme.ContentAddIcon(), func() {
		dc.addGatewayCard(cardBox, reloadCards)
	})
	if c != nil && !c.CanGatewayAddCard {
		cardAdd.Disable()
	}

	content := container.NewVScroll(container.NewVBox(
		card(container.NewVBox(
			container.NewBorder(nil, nil, sectionTitle("Keypad PINs"), pinAdd),
			captionWrap("All keypad PINs (permanent + time-limited, shown per PIN). Adding here "+
				"programs a TIME-LIMITED PIN through the gateway — for a permanent PIN, add it from "+
				"the Bluetooth tab."),
			pinBox,
		)),
		card(container.NewVBox(
			container.NewBorder(nil, nil, sectionTitle("RFID cards"), cardAdd),
			captionWrap("Access cards held to the reader. Adding here enrols a TIME-LIMITED card "+
				"through the gateway — to add a permanent card and tie it to a user, use the "+
				"Bluetooth tab."),
			cardBox,
		)),
	))

	reloadPins()
	reloadCards()
	openView(dc.win, "Keypad PINs & cards", content)
}

// loadKeypadPins returns a reload closure that repopulates box with EVERY keypad PIN — the
// unified list (bff.MergeKeypadPins) of permanent member PINs + time-limited gateway PINs —
// grouped by owner, each row badged Permanent/Expires and deletable by the right mechanism.
func (dc *deviceCtx) loadKeypadPins(box *fyne.Container) func() {
	// pins is the in-memory model. A confirmed delete drops the row from it and re-renders
	// (optimistic) — we never re-read the eventually-consistent gateway list, which would
	// still return the just-deleted row for a beat and make it flicker back.
	var pins []app.PinEntry
	var render func()

	remove := func(target app.PinEntry) {
		kept := pins[:0]
		for _, p := range pins {
			if p.ByIndex == target.ByIndex && p.Key == target.Key {
				continue
			}
			kept = append(kept, p)
		}
		pins = kept
		render()
	}

	// render rebuilds box from the current model, grouped by owner. No network.
	render = func() {
		box.Objects = nil
		if len(pins) == 0 {
			box.Add(emptyState(theme.AccountIcon(), "No keypad PINs yet."))
			box.Refresh()
			return
		}
		order := []string{}
		byOwner := map[string][]app.PinEntry{}
		for _, p := range pins {
			o := p.Owner
			if o == "" {
				o = "Unknown"
			}
			if _, ok := byOwner[o]; !ok {
				order = append(order, o)
			}
			byOwner[o] = append(byOwner[o], p)
		}
		for _, o := range order {
			box.Add(userGroupHeader(o, len(byOwner[o])))
			for _, p := range byOwner[o] {
				box.Add(indented(dc.gatewayPinRow(p, box, render, remove), 24))
			}
		}
		box.Refresh()
	}

	// Every keypad PIN, merged from the two lists WeLock splits them across (permanent
	// member PINs + time-limited gateway PINs), so nothing is hidden.
	reload := func() {
		box.Objects = []fyne.CanvasObject{spinnerBox()}
		box.Refresh()
		runAsync(dc.win,
			func() ([]app.PinEntry, error) {
				return dc.core.GatewayCredentials(context.Background(), dc.num(), dc.name())
			},
			func(fetched []app.PinEntry) { pins = fetched; render() },
			dc.fail,
		)
	}
	return reload
}

// gatewayPinRow renders one keypad PIN. It deletes by the mechanism its source list
// supports: a gateway/temporary PIN by its Index (UfunDeletePassword, async — unprograms
// the lock); a permanent member PIN by its Number (DeleteMember, a cloud-record delete).
// On a confirmed delete it drops the row via remove(); render() restores it if the command
// fails (busyBox blanks the list while the async command runs).
func (dc *deviceCtx) gatewayPinRow(p app.PinEntry, box *fyne.Container, render func(), remove func(app.PinEntry)) fyne.CanvasObject {
	sub := "Permanent"
	if !p.Permanent {
		sub = "Expires " + fmtUnix(p.EndTime)
	}
	del := subtleButton("", theme.DeleteIcon(), func() {
		confirm(dc.win, "Remove this keypad PIN?", func() {
			busyBox(box)
			if p.ByIndex {
				dc.runGatewayCommand("Remove PIN", func() (string, error) {
					return dc.core.UfunDeletePassword(context.Background(), dc.num(), dc.name(), p.Key)
				}, func(outcome string) {
					if outcome == "success" {
						remove(p)
					} else {
						render() // failed/unknown — restore the list busyBox cleared
					}
				})
			} else {
				runAsync(dc.win,
					func() (struct{}, error) {
						return struct{}{}, dc.core.DeleteMember(context.Background(), dc.num(), dc.name(), "Password", p.Key)
					},
					func(struct{}) { remove(p) },
					func(err error) { render(); dc.fail(err) },
				)
			}
		})
	})
	return manageRow("Keypad PIN", sub, del)
}

// loadGatewayCards returns a reload closure that repopulates box with the lock's RFID cards
// (member-list Card credentials), grouped by owner, each removable through the gateway.
func (dc *deviceCtx) loadGatewayCards(box *fyne.Container) func() {
	// groups is the in-memory model; a confirmed delete drops the card from it and
	// re-renders (optimistic), so we never re-read the eventually-consistent gateway list.
	var groups []app.OwnerGroup
	var render func()

	remove := func(target app.Credential) {
		for gi := range groups {
			kept := groups[gi].Creds[:0]
			for _, cr := range groups[gi].Creds {
				if cr.TypeName == "Card" && cr.Number == target.Number {
					continue
				}
				kept = append(kept, cr)
			}
			groups[gi].Creds = kept
		}
		render()
	}

	render = func() {
		box.Objects = nil
		n := 0
		for _, g := range groups {
			var cards []app.Credential
			for _, cr := range g.Creds {
				if cr.TypeName == "Card" {
					cards = append(cards, cr)
				}
			}
			if len(cards) == 0 {
				continue
			}
			owner := g.User
			if owner == "" {
				owner = "Unknown"
			}
			box.Add(userGroupHeader(owner, len(cards)))
			for _, cr := range cards {
				box.Add(indented(dc.gatewayCardRow(cr, box, render, remove), 24))
			}
			n += len(cards)
		}
		if n == 0 {
			box.Add(emptyState(theme.AccountIcon(), "No cards yet."))
		}
		box.Refresh()
	}

	reload := func() {
		box.Objects = []fyne.CanvasObject{spinnerBox()}
		box.Refresh()
		runAsync(dc.win,
			func() ([]app.OwnerGroup, error) {
				return dc.core.MembersGrouped(context.Background(), dc.num(), dc.name())
			},
			func(fetched []app.OwnerGroup) { groups = fetched; render() },
			dc.fail,
		)
	}
	return reload
}

// gatewayCardRow renders one RFID card, removed through the gateway by its number. On a
// confirmed delete it drops the row via remove(); render() restores it if the command fails.
func (dc *deviceCtx) gatewayCardRow(cr app.Credential, box *fyne.Container, render func(), remove func(app.Credential)) fyne.CanvasObject {
	// 0 or the far-future gateway "permanent" sentinel (≥2035-01-01) both mean no expiry.
	sub := "Permanent"
	if cr.EndTime > 0 && cr.EndTime < 2051222400 {
		sub = "Expires " + fmtUnix(cr.EndTime)
	}
	del := subtleButton("", theme.DeleteIcon(), func() {
		confirm(dc.win, "Remove this card?", func() {
			busyBox(box)
			dc.runGatewayCommand("Remove card", func() (string, error) {
				return dc.core.UfunDeleteCardNo(context.Background(), dc.num(), dc.name(), cr.Number)
			}, func(outcome string) {
				if outcome == "success" {
					remove(cr)
				} else {
					render() // failed/unknown — restore the list busyBox cleared
				}
			})
		})
	})
	return manageRow("RFID card   "+orDash(cr.Number), sub, del)
}

// reloadSoon reloads now and again a few seconds later. The gateway/temporary PIN list is
// eventually consistent — a just-confirmed add takes a couple seconds to show up — so an
// immediate reload alone misses it.
func reloadSoon(reload func()) {
	reload()
	go func() {
		time.Sleep(3 * time.Second)
		fyne.Do(reload)
	}()
}

// busyBox replaces a list box's contents with a spinner, so an async gateway command (which
// polls CommandStatus for up to ~20s) shows progress until its reload repopulates the box.
func busyBox(box *fyne.Container) {
	box.Objects = []fyne.CanvasObject{spinnerBox()}
	box.Refresh()
}

// addGatewayPin opens the add-temporary-PIN form. Gateway PINs are time-limited and carry a
// free-text LABEL (the remark), NOT a user — associating a credential to a user is a
// Bluetooth-only operation. On confirm it issues UfunSetPassword and reloads on completion.
func (dc *deviceCtx) addGatewayPin(box *fyne.Container, reload func()) {
	pin := widget.NewEntry()
	pin.SetPlaceHolder("e.g. 482913 — the code typed at the keypad")
	presets := presetSelectExcept(dc.core, nil, "permanent")
	label := widget.NewEntry()
	label.SetPlaceHolder("a label, e.g. Cleaner (optional)")
	openForm(dc.win, "Add temporary PIN",
		[]*widget.FormItem{
			widget.NewFormItem("PIN", pin),
			widget.NewFormItem("Validity", presets.control()),
			widget.NewFormItem("Label", label),
		},
		func() {
			if msg := dc.core.ValidatePin(dc.item.DeviceModel, pin.Text); msg != "" {
				dc.toast("Invalid PIN", msg)
				return
			}
			start, end, wmsg := presets.window()
			if wmsg != "" {
				dc.toast("Add PIN", wmsg)
				return
			}
			// The gateway op writes the cloud record itself (label = remark, a server-assigned
			// slot as its index), so there is NO AddMember mirror — delete keys on the gateway's
			// own index. See reverse-engineering/docs/CREDENTIALS.md §2a.
			busyBox(box)
			dc.runGatewayCommand("Add PIN", func() (string, error) {
				return dc.core.UfunSetPassword(context.Background(), dc.num(), dc.name(), strings.TrimSpace(pin.Text), 0, start, end, strings.TrimSpace(label.Text))
			}, func(outcome string) {
				if outcome == "success" {
					dc.core.Track("credential_added", map[string]any{"kind": "pin", "via": "remote"})
				}
				// Add can't be optimistic: the new row needs the server-assigned index (to
				// delete it later), so re-read once the eventually-consistent list settles.
				reloadSoon(reload)
			})
		})
}

// addGatewayCard opens the add-card form: a card number and a validity preset. On confirm it
// issues UfunAddCard and reloads on completion.
func (dc *deviceCtx) addGatewayCard(box *fyne.Container, reload func()) {
	cardNum := widget.NewEntry()
	cardNum.SetPlaceHolder("card number")
	presets := presetSelectExcept(dc.core, nil, "permanent")
	label := widget.NewEntry()
	label.SetPlaceHolder("a label (optional)")
	openForm(dc.win, "Add temporary card",
		[]*widget.FormItem{
			widget.NewFormItem("Card number", cardNum),
			widget.NewFormItem("Validity", presets.control()),
			widget.NewFormItem("Label", label),
		},
		func() {
			l := strings.TrimSpace(label.Text)
			start, end, wmsg := presets.window()
			if wmsg != "" {
				dc.toast("Add card", wmsg)
				return
			}
			cn := strings.TrimSpace(cardNum.Text)
			// condition 255 = "every day". UfunAddCard carries no label field, so we mirror a
			// cloud record filed under the label (its `user` slot) with the card number as the
			// record number — matching how it's deleted (UfunDeleteCardNo by number). Gateway
			// cards are labeled, not user-associated (that's Bluetooth-only). See CREDENTIALS.md §2a.
			busyBox(box)
			dc.runGatewayCommand("Add card", func() (string, error) {
				return dc.core.UfunAddCard(context.Background(), dc.num(), dc.name(), cn, start, end, 255)
			}, func(outcome string) {
				if outcome == "success" {
					dc.fileMember("Card", l, cn) // don't mirror a card the gateway rejected
					dc.core.Track("credential_added", map[string]any{"kind": "card", "via": "remote"})
				}
				reloadSoon(reload) // restores the list busyBox cleared, success or not
			})
		})
}

// --- command helpers ------------------------------------------------------

// runCommand runs a bare-command-id op, then polls CommandStatus to a terminal state,
// reflecting each phase in statusLbl. Optional onDone callbacks receive the terminal
// outcome ("success"/"failed"/"unknown"), used for analytics.
func (dc *deviceCtx) runCommand(statusLbl *widget.Label, label string, op func() (string, error), onDone ...func(outcome string)) {
	done := func(outcome string) {
		for _, f := range onDone {
			if f != nil {
				f(outcome)
			}
		}
	}
	statusLbl.SetText(label + "…")
	runAsync(dc.win, op,
		func(id string) {
			id = strings.TrimSpace(id)
			if id == "" {
				statusLbl.SetText(label + ": accepted")
				done("success")
				return
			}
			statusLbl.SetText(label + ": relaying…")
			dc.pollCommand(id, func(outcome string) {
				statusLbl.SetText(label + ": " + outcome)
				done(outcome)
			})
		},
		func(err error) { statusLbl.SetText(label + " failed"); dc.fail(err) },
	)
}

// runGatewayCommand runs a bare-command-id op inside a modal (no status label), surfaces the
// polled outcome as a toast, and invokes onFinish (may be nil) once the command settles — used
// to reload the credentials list after an add/remove.
func (dc *deviceCtx) runGatewayCommand(label string, op func() (string, error), onFinish func(outcome string)) {
	runAsync(dc.win, op,
		func(id string) {
			id = strings.TrimSpace(id)
			if id == "" {
				// Accepted synchronously (no command to poll) — treat as success.
				dc.toast(label, "Accepted.")
				if onFinish != nil {
					onFinish("success")
				}
				return
			}
			dc.pollCommand(id, func(outcome string) {
				dc.toast(label, label+": "+outcome)
				if onFinish != nil {
					onFinish(outcome)
				}
			})
		},
		dc.fail,
	)
}

// looseView renders a loose map as sorted key/value rows for a result modal, or an empty state.
func looseView(m map[string]any) fyne.CanvasObject {
	if len(m) == 0 {
		return emptyState(theme.InfoIcon(), "No data.")
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := container.NewVBox()
	for _, k := range keys {
		rows.Add(kv(k, fmt.Sprint(m[k])))
	}
	return container.NewVScroll(rows)
}
