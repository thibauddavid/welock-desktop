package ui

import (
	"context"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/thibauddavid/welock-desktop/internal/app"
)

// deviceCtx is the per-device context shared by the detail tabs.
type deviceCtx struct {
	*screen
	item app.DeviceListItem
	info *app.Device // loaded lazily from DeviceInfo; may be nil

	// capsListeners are invoked (on the main goroutine) once the shaped DeviceInfo —
	// and therefore the capability set — has loaded, so each tab can re-gate its
	// gateway/BLE actions. They are registered via onCaps and fired by capsLoaded.
	capsListeners []func()
}

func (d *deviceCtx) num() string  { return d.item.DeviceNumber }
func (d *deviceCtx) name() string { return d.item.DeviceName }

// caps returns the device capabilities, or nil if not yet loaded. A nil pointer means
// "unknown" — the UI then assumes an action is allowed (mirrors the web `?? true`).
func (d *deviceCtx) caps() *app.Capabilities {
	if d.info == nil {
		return nil
	}
	return d.info.Capabilities
}

// onCaps registers a listener that re-applies capability gating. It is invoked
// immediately (with the current, possibly-unknown state) and again once DeviceInfo
// loads, so a tab can build enabled-by-default controls and let real capabilities
// disable the unsupported ones.
func (d *deviceCtx) onCaps(fn func()) {
	d.capsListeners = append(d.capsListeners, fn)
	fn()
}

// capsLoaded fires every registered capability listener (after DeviceInfo resolves).
func (d *deviceCtx) capsLoaded() {
	for _, fn := range d.capsListeners {
		fn()
	}
}

// newDeviceDetail builds the master-detail right pane for one lock: a header card with
// the name + battery meter and a three-tab body (Bluetooth / Remote / Manage). The
// Bluetooth tab is disabled with a notice when the build has no radio.
func newDeviceDetail(s *screen, item app.DeviceListItem, initialTab int) fyne.CanvasObject {
	dc := &deviceCtx{screen: s, item: item}

	name := item.DeviceName
	if name == "" {
		name = item.DeviceNumber
	}
	meter := newBatteryMeter()
	if item.Battery != nil {
		meter.Set(*item.Battery, true)
	}

	titleBlock := container.NewHBox(
		container.NewCenter(widget.NewIcon(theme.HomeIcon())),
		container.NewVBox(h2(name), caption(item.DeviceNumber)),
	)

	// Device-level and destructive actions live in a quiet overflow menu on the header
	// (right of the battery meter) so the tab bodies can keep only their primary actions.
	overflow := overflowButton(
		fyne.NewMenuItem("Rename…", func() {
			entry := widget.NewEntry()
			entry.SetText(dc.name())
			openForm(dc.win, "Rename device",
				[]*widget.FormItem{widget.NewFormItem("Name", entry)},
				func() {
					newName := entry.Text
					runAsync(dc.win,
						func() (struct{}, error) {
							return struct{}{}, dc.core.RenameDevice(context.Background(), dc.num(), dc.name(), newName)
						},
						func(struct{}) {
							dc.toast("Device", "Renamed.")
							if dc.refresh != nil {
								dc.refresh()
							}
						},
						dc.fail,
					)
				})
		}),
		fyne.NewMenuItem("Transfer ownership…", func() {
			account := widget.NewEntry()
			areacode := widget.NewEntry()
			openForm(dc.win, "Transfer ownership",
				[]*widget.FormItem{
					widget.NewFormItem("Account", account),
					widget.NewFormItem("Area code", areacode),
				},
				func() {
					acct, ac := account.Text, areacode.Text
					runAsync(dc.win,
						func() (struct{}, error) {
							return struct{}{}, dc.core.TransferPermission(context.Background(), dc.num(), acct, ac)
						},
						func(struct{}) {
							dc.toast("Device", "Ownership transferred.")
							if dc.refresh != nil {
								dc.refresh()
							}
						},
						dc.fail,
					)
				})
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Delete device", func() {
			confirm(dc.win, "Delete this device?", func() {
				runAsync(dc.win,
					func() (struct{}, error) {
						return struct{}{}, dc.core.DeleteDevice(context.Background(), dc.num(), dc.name())
					},
					func(struct{}) {
						dc.toast("Device", "Deleted.")
						if dc.refresh != nil {
							dc.refresh()
						}
						if dc.home != nil {
							dc.home()
						}
					},
					dc.fail,
				)
			})
		}),
	)

	header := card(container.NewBorder(nil, nil, titleBlock,
		container.NewHBox(container.NewCenter(meter.Object()), container.NewCenter(overflow))))

	bt := newBluetoothTab(dc)
	remote := newRemoteTab(dc)
	manage := newManageTab(dc)

	btTab := container.NewTabItemWithIcon("Bluetooth", theme.ComputerIcon(), bt)
	remoteTab := container.NewTabItemWithIcon("Remote", theme.MailSendIcon(), remote)
	manageTab := container.NewTabItemWithIcon("Manage", theme.SettingsIcon(), manage)

	tabs := container.NewAppTabs(btTab, remoteTab, manageTab)
	tabs.SetTabLocation(container.TabLocationTop)
	if !s.core.BleAvailable() {
		tabs.DisableItem(btTab)
	}
	if initialTab > 0 && initialTab < len(tabs.Items) {
		tabs.SelectIndex(initialTab)
	}

	// Lazily refine battery from the shaped DeviceInfo.
	runAsync(s.win,
		func() (*app.Device, error) { return s.core.DeviceInfo(context.Background(), item.DeviceNumber) },
		func(d *app.Device) {
			if d == nil {
				return
			}
			dc.info = d
			if d.Battery != nil {
				meter.Set(*d.Battery, true)
			}
			dc.capsLoaded()
		},
		func(err error) { /* non-fatal: keep the list-row battery */ },
	)

	return container.NewBorder(container.NewVBox(header), nil, nil, nil, tabs)
}
