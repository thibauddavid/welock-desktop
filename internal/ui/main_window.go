package ui

import (
	"context"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/thibauddavid/welock-desktop/internal/app"
)

// mainView holds the master-detail state for the signed-in app.
type mainView struct {
	*screen

	locks    []app.DeviceListItem
	gateways []app.Gateway

	lockBox     *fyne.Container // VBox of tappable lock rows
	gwBox       *fyne.Container // VBox of tappable gateway rows
	detail      *fyne.Container
	selectedKey string // "lock:<num>" / "gw:<num>" — for the row highlight
}

// NewMainWindow builds the signed-in master-detail window: a branded left rail listing
// locks + gateways, and a right pane showing the selected device on the app surface.
// onLogout swaps the root content back to the login screen (also used on auth failure).
func NewMainWindow(a fyne.App, win fyne.Window, core *app.Core, onLogout func()) fyne.CanvasObject {
	_, root := buildMain(a, win, core, onLogout)
	return root
}

// buildMain constructs the master-detail main view and its root container. Returning the
// *mainView lets the screenshot harness seed data and select a device.
func buildMain(a fyne.App, win fyne.Window, core *app.Core, onLogout func()) (*mainView, fyne.CanvasObject) {
	m := &mainView{
		screen: &screen{app: a, win: win, core: core, toLogin: onLogout},
	}

	m.detail = container.NewStack(placeholder("Select a lock or gateway"))
	m.lockBox = container.NewVBox()
	m.gwBox = container.NewVBox()

	// Detail-pane actions (rename/transfer/delete in the device header) keep the sidebar in
	// sync; home resets the pane and clears the selection after a delete.
	m.screen.refresh = m.refresh
	m.screen.home = func() {
		m.selectedKey = ""
		m.renderLists()
		m.showDetail(placeholder("Select a lock or gateway"))
	}

	// Brand header on top of the rail.
	brand := container.NewHBox(logo(24), h2("WeLock"))
	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), m.refresh)
	refreshBtn.Importance = widget.LowImportance
	addBtn := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() { m.showAddMenu() })
	addBtn.Importance = widget.LowImportance
	header := container.NewBorder(nil, nil, brand, container.NewHBox(refreshBtn, addBtn))

	lists := container.NewVBox(
		container.NewPadded(sectionTitle("Locks")),
		m.lockBox,
		container.NewPadded(sectionTitle("Gateways")),
		m.gwBox,
	)

	// Bottom account bar.
	settingsBtn := subtleButton("Settings", theme.SettingsIcon(), func() {
		m.selectedKey = ""
		m.renderLists()
		m.showDetail(newSettings(m.screen))
	})
	logoutBtn := subtleButton("Sign out", theme.LogoutIcon(), m.logout)
	bottom := container.NewVBox(widget.NewSeparator(), container.NewGridWithColumns(2, settingsBtn, logoutBtn))

	railContent := container.NewBorder(
		container.NewPadded(header),
		container.NewPadded(bottom),
		nil, nil,
		container.NewVScroll(lists),
	)
	rail := container.NewStack(canvas.NewRectangle(colSidebar), railContent)

	split := container.NewHSplit(rail, container.NewPadded(m.detail))
	split.SetOffset(0.28)

	m.renderLists()
	m.refresh()
	return m, split
}

// deviceRow is a tappable sidebar row: a vertically-centered leading icon, a bold name
// over a muted subtitle, and a subtle tint when selected.
func deviceRow(icon fyne.Resource, title, subtitle string, selected bool, onTap func()) fyne.CanvasObject {
	ic := widget.NewIcon(icon)
	name := canvas.NewText(title, colText)
	name.TextStyle = fyne.TextStyle{Bold: true}
	name.TextSize = 14
	sub := canvas.NewText(subtitle, colMuted)
	sub.TextSize = 11
	// Both title and subtitle are canvas.Text so they share the same left origin (a
	// widget.Label would add internal padding and shift the title right of the subtitle).
	text := container.NewVBox(name, sub)

	// Icon vertically centered in a full-height left column; text stays left-aligned.
	inner := container.NewBorder(nil, nil, container.NewPadded(container.NewCenter(ic)), nil, text)

	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 8
	if selected {
		bg.FillColor = withAlpha(colBrand, 0x1E)
	}
	return newTappable(container.NewStack(bg, container.NewPadded(inner)), onTap)
}

// renderLists rebuilds the lock + gateway rows from the current data + selection.
func (m *mainView) renderLists() {
	m.lockBox.Objects = nil
	if len(m.locks) == 0 {
		m.lockBox.Add(container.NewPadded(caption("No locks yet.")))
	}
	for _, it := range m.locks {
		it := it
		name := it.DeviceName
		if name == "" {
			name = it.DeviceNumber
		}
		key := "lock:" + it.DeviceNumber
		m.lockBox.Add(deviceRow(theme.HomeIcon(), name, it.DeviceNumber, m.selectedKey == key, func() {
			m.selectedKey = key
			m.renderLists()
			m.showDetail(newDeviceDetail(m.screen, it, 0))
		}))
	}
	m.lockBox.Refresh()

	m.gwBox.Objects = nil
	if len(m.gateways) == 0 {
		m.gwBox.Add(container.NewPadded(caption("No gateways yet.")))
	}
	for _, g := range m.gateways {
		g := g
		sub := g.GatewayModel
		if sub == "" {
			sub = "Gateway"
		}
		if !g.Online {
			sub = "Offline"
		}
		key := "gw:" + g.DeviceNumber
		m.gwBox.Add(deviceRow(theme.ComputerIcon(), g.DeviceName, sub, m.selectedKey == key, func() {
			m.selectedKey = key
			m.renderLists()
			m.showDetail(m.gatewayPanel(g))
		}))
	}
	m.gwBox.Refresh()
}

// showDetail swaps the right pane content.
func (m *mainView) showDetail(o fyne.CanvasObject) {
	m.detail.Objects = []fyne.CanvasObject{o}
	m.detail.Refresh()
}

// refresh reloads the lock + gateway lists.
func (m *mainView) refresh() {
	runAsync(m.win,
		func() ([]app.DeviceListItem, error) { return m.core.Devices(context.Background()) },
		func(items []app.DeviceListItem) {
			locks := items[:0:0]
			for _, it := range items {
				if it.Type == 6 {
					continue // gateway row; gateways come from Gateways()
				}
				locks = append(locks, it)
			}
			m.locks = locks
			m.renderLists()
		},
		m.fail,
	)
	runAsync(m.win,
		func() ([]app.Gateway, error) { return m.core.Gateways(context.Background()) },
		func(gws []app.Gateway) {
			m.gateways = gws
			m.renderLists()
		},
		m.fail,
	)
}

// logout confirms, clears the session and returns to login.
func (m *mainView) logout() {
	confirmLogout(m.win, func() {
		if err := m.core.Logout(); err != nil {
			m.fail(err)
			return
		}
		m.toLogin()
	})
}

// showAddMenu offers the add-device / add-gateway actions.
func (m *mainView) showAddMenu() {
	newAddChooser(m.screen, m.refresh)
}

// gatewayPanel renders a gateway summary card with status/rename/delete actions.
func (m *mainView) gatewayPanel(g app.Gateway) fyne.CanvasObject {
	info := card(container.NewVBox(
		container.NewBorder(nil, nil, nil, statusPill(g.Online), h2(orDash(g.DeviceName))),
		widget.NewSeparator(),
		kv("Model", g.GatewayModel),
		kv("Type", g.GwType),
		kv("Owned", yesNo(g.Owned)),
		func() fyne.CanvasObject {
			if g.Bridges != "" {
				return kv("Bridges", g.Bridges)
			}
			return widget.NewLabel("")
		}(),
	))

	body := container.NewVBox(pageHeader("Gateway", g.DeviceNumber), info)

	if g.Owned {
		value := g.DeviceNumber
		rename := subtleButton("Rename", theme.DocumentCreateIcon(), func() {
			promptText(m.win, "Rename gateway", "New name", g.DeviceName, func(name string) {
				runAsync(m.win,
					func() (struct{}, error) {
						return struct{}{}, m.core.AlterGatewayName(context.Background(), name, value)
					},
					func(struct{}) { m.toast("Gateway", "Renamed."); m.refresh() },
					m.fail,
				)
			})
		})
		status := subtleButton("Status", theme.InfoIcon(), func() {
			runAsync(m.win,
				func() (string, error) { return m.core.GatewaysStatus(context.Background(), value) },
				func(js string) { m.toast("Gateway status", orDash(js)) },
				m.fail,
			)
		})
		del := dangerButton("Delete", theme.DeleteIcon(), func() {
			confirm(m.win, "Delete gateway?", func() {
				runAsync(m.win,
					func() (struct{}, error) { return struct{}{}, m.core.DeleteGateway(context.Background(), value) },
					func(struct{}) {
						m.toast("Gateway", "Deleted.")
						m.refresh()
						m.showDetail(placeholder("Select a lock or gateway"))
					},
					m.fail,
				)
			})
		})
		body.Add(card(container.NewVBox(sectionTitle("Actions"), container.NewHBox(rename, status, del))))
	}

	return container.NewVScroll(body)
}
