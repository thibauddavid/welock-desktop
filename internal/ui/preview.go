//go:build screenshots

// Package ui — screenshot-only preview helpers (build tag `screenshots`).
//
// These compose each screen INTO the full main window (sidebar + header) so the README
// screenshots show real context, and expose a hook to drive the unlock flow for the GIF.
// They are gated behind the `screenshots` build tag so they never compile into the shipped
// app; the screenshot tool (cmd/screenshots) builds with `-tags "screenshots tinygobt"`.
// Regenerate everything with tools/screenshots/capture.sh.
package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/thibauddavid/welock-desktop/internal/app"
)

// PreviewLogin returns the sign-in screen (no data needed).
func PreviewLogin(a fyne.App, w fyne.Window, core *app.Core) fyne.CanvasObject {
	return NewLogin(a, w, core, func() {})
}

func previewCtx(a fyne.App, w fyne.Window, core *app.Core) *deviceCtx {
	b := 87
	item := app.DeviceListItem{DeviceNumber: "10002345", DeviceName: "Front Door", Battery: &b}
	return &deviceCtx{screen: &screen{app: a, win: w, core: core, toLogin: func() {}}, item: item}
}

// PreviewMain builds the full signed-in window with a fake lock + gateway and selects a
// device showing the requested feature ("locks" | "remote" | "manage" | "bluetooth" |
// "gateway" | "settings").
func PreviewMain(a fyne.App, w fyne.Window, core *app.Core, feature string) fyne.CanvasObject {
	m, root := buildMain(a, w, core, func() {})
	b := 87
	m.locks = []app.DeviceListItem{{DeviceNumber: "10002345", DeviceName: "Front Door", Battery: &b}}
	m.gateways = []app.Gateway{{DeviceNumber: "70001122", DeviceName: "Hallway Hub", GatewayModel: "WIFIBOX3", GwType: "1", Owned: true, Online: true}}

	selectLockTab := func(tab int) {
		m.selectedKey = "lock:10002345"
		m.renderLists()
		m.showDetail(newDeviceDetail(m.screen, m.locks[0], tab))
	}
	switch feature {
	case "locks":
		m.renderLists()
	case "bluetooth":
		selectLockTab(0)
	case "manage":
		selectLockTab(2)
	case "gateway":
		m.selectedKey = "gw:70001122"
		m.renderLists()
		m.showDetail(m.gatewayPanel(m.gateways[0]))
	case "settings":
		m.renderLists()
		m.showDetail(newSettings(m.screen))
	default: // "remote"
		selectLockTab(1)
	}
	return root
}

// PreviewOpenModal opens a modal over the composed main window (set to the Manage tab), so
// the screenshot shows the modal in context.
func PreviewOpenModal(a fyne.App, w fyne.Window, core *app.Core, which string) {
	dc := previewCtx(a, w, core)
	switch which {
	case "temp":
		dc.showTempPasswords()
	case "activity":
		dc.showActivity()
	case "creds":
		dc.showGatewayCredentials()
	case "addmember", "addpin":
		dc.addGatewayPin(container.NewVBox(), func() {})
	case "addchooser":
		newAddChooser(dc.screen, func() {})
	default:
		dc.showPeople()
	}
}

// PreviewTapUnlock finds the visible Remote-tab "Unlock" button and invokes it, so the
// harness can record the unlock flow (relaying → confirmed) without a real click.
func PreviewTapUnlock(w fyne.Window) {
	var find func(o fyne.CanvasObject) *widget.Button
	find = func(o fyne.CanvasObject) *widget.Button {
		switch v := o.(type) {
		case *widget.Button:
			if v.Text == "Unlock" {
				return v
			}
		case *fyne.Container:
			for _, c := range v.Objects {
				if b := find(c); b != nil {
					return b
				}
			}
		case *container.Scroll:
			return find(v.Content)
		case *container.Split:
			if b := find(v.Leading); b != nil {
				return b
			}
			return find(v.Trailing)
		case *container.AppTabs:
			if sel := v.Selected(); sel != nil { // only the visible (Remote) tab
				return find(sel.Content)
			}
		}
		return nil
	}
	if b := find(w.Content()); b != nil && b.OnTapped != nil {
		b.OnTapped()
	}
}
