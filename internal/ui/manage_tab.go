package ui

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/thibauddavid/welock-desktop/internal/app"
)

// manageState tracks the paginated unlock-records cursor for one Activity modal.
type manageState struct {
	recPage int
	records []app.UnlockRecord
}

// newManageTab builds the administration tab as a short, scannable drill-in menu rather
// than a wall of stacked cards. Each row opens a focused modal that carries its own list,
// per-row actions, and a primary "Add". Device-level and destructive actions (rename /
// transfer / delete) now live in the detail-header overflow menu, not here.
func newManageTab(dc *deviceCtx) fyne.CanvasObject {
	body := container.NewVBox(
		navRow(theme.AccountIcon(), "People & access", "Who's enrolled and who it's shared with",
			func() { dc.showPeople() }),
		navRow(theme.DocumentIcon(), "Offline codes", "Time-limited codes the lock computes offline",
			func() { dc.showTempPasswords() }),
		navRow(theme.HistoryIcon(), "Activity", "Unlock history",
			func() { dc.showActivity() }),
	)
	return container.NewVScroll(container.NewPadded(body))
}

// showTempPasswords opens the temp-password modal: the current codes (each revocable) with
// a primary "Add" that generates a time-limited PIN and reveals it once.
func (dc *deviceCtx) showTempPasswords() {
	box := container.NewVBox()
	reload := dc.loadTempPasswords(box)

	add := primaryButton("Generate", theme.ContentAddIcon(), func() {
		newAddTempPassword(dc, reload)
	})
	content := container.NewBorder(
		container.NewVBox(
			captionWrap("Offline codes are computed by the lock from its clock. For a fresh code to "+
				"open the lock, the lock's clock must be current — it's synced automatically when you "+
				"generate one (through the gateway). The code is revealed once. Deleting a code hides "+
				"the record but does NOT stop it working at the lock (only rotating the lock's key "+
				"revokes all offline codes at once)."),
			container.NewHBox(add),
		),
		nil, nil, nil,
		container.NewVScroll(box),
	)

	reload()
	openView(dc.win, "Offline codes", content)
}

// syncLockClock best-effort sets the lock's clock to now through the gateway. Offline codes'
// validity is computed against real wall-clock, so a drifted lock clock rejects fresh codes.
// Fire-and-forget; skipped when the lock has no gateway.
func (dc *deviceCtx) syncLockClock() {
	if c := dc.caps(); c != nil && !c.SupportsGatewayUnlock {
		return
	}
	runAsync(dc.win,
		func() (string, error) {
			return dc.core.UfunSetLockTime(context.Background(), dc.num(), dc.name(), "", 0)
		},
		func(string) {},
		func(error) {},
	)
}

// showPeople opens the people & access modal: two sections — Members (credentials stored on
// the lock) and Permissions (accounts the lock is shared with) — each with per-row removal
// and its own primary "Add".
func (dc *deviceCtx) showPeople() {
	memBox := container.NewVBox()
	permBox := container.NewVBox()
	reloadMembers := dc.loadMembersGrouped(memBox)
	reloadPerms := dc.loadPermissions(permBox)

	addPerm := primaryButton("Add", theme.ContentAddIcon(), func() {
		newAddPermission(dc, reloadPerms)
	})

	// Members is a read-only view. Adding/removing a credential must program the lock —
	// which happens via the gateway (Remote tab: PIN & card) or over BLE (Bluetooth tab:
	// fingerprint). The cloud member record on its own doesn't touch the lock, so we don't
	// offer add/remove here (matches the web client).
	members := card(container.NewVBox(
		sectionTitle("Members"),
		caption("Credentials stored on the lock, grouped by person. To add or remove one, "+
			"use the Remote tab (PIN & card) or the Bluetooth tab (fingerprint)."),
		memBox,
	))
	perms := card(container.NewVBox(
		container.NewBorder(nil, nil, sectionTitle("Permissions"), addPerm),
		caption("Accounts you have shared this lock with."),
		permBox,
	))

	reloadMembers()
	reloadPerms()
	openView(dc.win, "People & access", container.NewVScroll(container.NewVBox(members, perms)))
}

// showActivity opens the unlock-history modal with paginated records and a "Load more" button.
func (dc *deviceCtx) showActivity() {
	st := &manageState{}
	recBox := container.NewVBox()

	loadMore := subtleButton("Load more", theme.MoveDownIcon(), nil)
	loadMore.OnTapped = func() {
		st.recPage++
		loadMore.Disable()
		runAsync(dc.win,
			func() ([]app.UnlockRecord, error) {
				return dc.core.UnlockRecords(context.Background(), dc.num(), st.recPage, 20)
			},
			func(recs []app.UnlockRecord) {
				loadMore.Enable()
				if len(recs) == 0 {
					if len(st.records) == 0 {
						recBox.Objects = nil
						recBox.Add(emptyState(theme.HistoryIcon(), "No unlock records yet."))
						recBox.Refresh()
						return
					}
					dc.toast("Records", "No more records.")
					return
				}
				if len(st.records) == 0 {
					recBox.Objects = nil // clear any prior empty-state
				}
				st.records = append(st.records, recs...)
				for _, r := range recs {
					recBox.Add(recordRow(r))
				}
				recBox.Refresh()
			},
			func(err error) { loadMore.Enable(); dc.fail(err) },
		)
	}

	content := container.NewBorder(
		nil,
		container.NewCenter(loadMore),
		nil, nil,
		container.NewVScroll(recBox),
	)

	recBox.Objects = []fyne.CanvasObject{spinnerBox()}
	recBox.Refresh()
	loadMore.OnTapped() // load first page
	openView(dc.win, "Activity", content)
}

// manageRow renders one list entry: a bold title over a muted subtitle, with an
// optional trailing action hugging the right edge. It is the shared row template for
// temp passwords, members, permissions, and unlock records.
func manageRow(title, subtitle string, trailing fyne.CanvasObject) fyne.CanvasObject {
	t := canvas.NewText(title, colText)
	t.TextStyle = fyne.TextStyle{Bold: true}
	t.TextSize = 14
	text := container.NewVBox(t, caption(subtitle))
	row := container.NewBorder(nil, nil, nil, trailing, text)
	return container.NewPadded(row)
}

// loadTempPasswords returns a reload closure that repopulates box with temp-password rows.
func (dc *deviceCtx) loadTempPasswords(box *fyne.Container) func() {
	return func() {
		box.Objects = []fyne.CanvasObject{spinnerBox()}
		box.Refresh()
		runAsync(dc.win,
			func() ([]app.TempPassword, error) {
				return dc.core.TempPasswords(context.Background(), dc.num(), dc.name())
			},
			func(tps []app.TempPassword) {
				box.Objects = nil
				if len(tps) == 0 {
					box.Add(emptyState(theme.DocumentIcon(), "No temporary passwords yet."))
				}
				for _, tp := range tps {
					box.Add(dc.tempPasswordRow(tp, box))
				}
				box.Refresh()
			},
			dc.fail,
		)
	}
}

// tempPasswordRow renders one temp password with a remove affordance.
func (dc *deviceCtx) tempPasswordRow(tp app.TempPassword, box *fyne.Container) fyne.CanvasObject {
	title := fmt.Sprintf("%s   %s", orDash(tp.Password), orDash(tp.Remark))
	sub := tp.Time
	if sub == "" {
		sub = fmt.Sprintf("%s → %s", fmtUnix(tp.StartTime), fmtUnix(tp.EndTime))
	}
	del := subtleButton("", theme.DeleteIcon(), func() {
		confirm(dc.win, "Delete this password?", func() {
			id := tp.ID
			runAsync(dc.win,
				func() (struct{}, error) {
					return struct{}{}, dc.core.DeleteTempPassword(context.Background(), dc.num(), dc.name(), id)
				},
				func(struct{}) { dc.loadTempPasswords(box)() },
				dc.fail,
			)
		})
	})
	return manageRow(title, sub, del)
}

// loadMembersGrouped returns a reload closure that repopulates box with credentials
// GROUPED BY OWNING USER (a bold owner header, then that user's credential rows),
// mirroring the web MembersPanel. Grouping is done in the core (app.GroupCredentialsByOwner).
func (dc *deviceCtx) loadMembersGrouped(box *fyne.Container) func() {
	var reload func()
	reload = func() {
		box.Objects = []fyne.CanvasObject{spinnerBox()}
		box.Refresh()
		runAsync(dc.win,
			func() ([]app.OwnerGroup, error) {
				return dc.core.MembersGrouped(context.Background(), dc.num(), dc.name())
			},
			func(groups []app.OwnerGroup) {
				box.Objects = nil
				total := 0
				for _, g := range groups {
					total += len(g.Creds)
				}
				if total == 0 {
					box.Add(emptyState(theme.AccountIcon(), "No members yet."))
				}
				for _, g := range groups {
					owner := g.User
					if owner == "" {
						owner = "Unknown"
					}
					box.Add(userGroupHeader(owner, len(g.Creds)))
					for _, c := range g.Creds {
						box.Add(indented(dc.memberRow(c, reload), 24))
					}
				}
				box.Refresh()
			},
			dc.fail,
		)
	}
	return reload
}

// userGroupHeader is the per-owner header row in the grouped members list: the owner's
// name in bold with their credential count.
func userGroupHeader(owner string, n int) fyne.CanvasObject {
	name := canvas.NewText(owner, colText)
	name.TextStyle = fyne.TextStyle{Bold: true}
	name.TextSize = 15
	cnt := canvas.NewText(fmt.Sprintf("%d", n), colMuted)
	cnt.TextSize = 12
	return container.NewPadded(container.NewBorder(nil, nil, name, container.NewCenter(cnt), nil))
}

// memberRow renders one credential (its type + number, expiry) with a delete affordance.
// Owner is omitted here — the row already sits under its owner's group header. reload
// repopulates the (grouped) list after a delete.
func (dc *deviceCtx) memberRow(c app.Credential, reload func()) fyne.CanvasObject {
	title := fmt.Sprintf("%s   %s", c.TypeLabel, orDash(c.Number))
	sub := "No expiry"
	if c.EndTime != 0 {
		sub = "Expires " + fmtUnix(c.EndTime)
	}
	del := subtleButton("", theme.DeleteIcon(), func() {
		confirm(dc.win, "Delete this credential?", func() {
			runAsync(dc.win,
				func() (struct{}, error) {
					return struct{}{}, dc.core.DeleteMember(context.Background(), dc.num(), dc.name(), c.TypeName, c.Number)
				},
				func(struct{}) { reload() },
				dc.fail,
			)
		})
	})
	return manageRow(title, sub, del)
}

// loadPermissions returns a reload closure that repopulates box with permission rows.
func (dc *deviceCtx) loadPermissions(box *fyne.Container) func() {
	return func() {
		box.Objects = []fyne.CanvasObject{spinnerBox()}
		box.Refresh()
		runAsync(dc.win,
			func() ([]app.Permission, error) {
				return dc.core.PermissionList(context.Background(), dc.num())
			},
			func(perms []app.Permission) {
				box.Objects = nil
				if len(perms) == 0 {
					box.Add(emptyState(theme.VisibilityIcon(), "Not shared with anyone."))
				}
				for _, p := range perms {
					box.Add(dc.permissionRow(p, box))
				}
				box.Refresh()
			},
			dc.fail,
		)
	}
}

// permissionRow renders one shared permission with a revoke affordance.
func (dc *deviceCtx) permissionRow(p app.Permission, box *fyne.Container) fyne.CanvasObject {
	title := orDash(p.Account)
	if p.NickName != "" {
		title = fmt.Sprintf("%s (%s)", p.NickName, p.Account)
	}
	sub := fmt.Sprintf("role %d   exp %s", p.RoleID, fmtUnix(p.EndTime))
	del := subtleButton("", theme.DeleteIcon(), func() {
		confirm(dc.win, "Revoke this share?", func() {
			runAsync(dc.win,
				func() (struct{}, error) {
					return struct{}{}, dc.core.DelPermission(context.Background(), dc.num(), p.Account)
				},
				func(struct{}) { dc.loadPermissions(box)() },
				dc.fail,
			)
		})
	})
	return manageRow(title, sub, del)
}

// recordRow renders one unlock record.
func recordRow(r app.UnlockRecord) fyne.CanvasObject {
	who := r.Name
	if who == "" {
		who = orDash(r.ActorID)
	}
	how := r.How
	if r.Remote {
		how = "remote " + how
	}
	title := fmt.Sprintf("%s   %s", orDash(r.Time), who)
	return manageRow(title, orDash(how), nil)
}
