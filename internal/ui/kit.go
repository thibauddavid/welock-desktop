package ui

// kit.go is the WeLock Desktop design system: the color/spacing tokens, the
// embedded brand asset, and a small set of reusable building blocks (cards,
// headings, buttons, pills, empty states, form fields) that EVERY screen composes
// from. Screens should reach for these helpers instead of hand-rolling layout, so
// the whole app reads as one consistent, modern surface.

import (
	_ "embed"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// --- brand asset ----------------------------------------------------------

//go:embed icon.png
var iconBytes []byte

// appIcon is the WeLock logo, reused for the app icon and the login hero.
var appIcon = fyne.NewStaticResource("welock.png", iconBytes)

// --- color tokens ---------------------------------------------------------

var (
	colBrand      = color.NRGBA{R: 0x0E, G: 0x7C, B: 0x86, A: 0xFF} // teal (matches the icon)
	colBrandDark  = color.NRGBA{R: 0x0A, G: 0x53, B: 0x5B, A: 0xFF}
	colBrandLight = color.NRGBA{R: 0x1C, G: 0xA1, B: 0xAD, A: 0xFF}

	colBg      = color.NRGBA{R: 0xF1, G: 0xF4, B: 0xF7, A: 0xFF} // window background
	colSurface = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // cards / inputs
	colSidebar = color.NRGBA{R: 0xFA, G: 0xFB, B: 0xFC, A: 0xFF} // left rail
	colText    = color.NRGBA{R: 0x18, G: 0x22, B: 0x2B, A: 0xFF} // primary text
	colMuted   = color.NRGBA{R: 0x69, G: 0x76, B: 0x83, A: 0xFF} // secondary text
	colBorder  = color.NRGBA{R: 0xE2, G: 0xE7, B: 0xEC, A: 0xFF} // hairlines

	colSuccess = color.NRGBA{R: 0x1F, G: 0xA9, B: 0x6B, A: 0xFF}
	colWarning = color.NRGBA{R: 0xD9, G: 0x94, B: 0x2B, A: 0xFF}
	colDanger  = color.NRGBA{R: 0xD9, G: 0x4F, B: 0x49, A: 0xFF}

	// battery chip colors (used by batteryMeter) — aliased to the status palette.
	batteryGreen = colSuccess
	batteryAmber = colWarning
	batteryRed   = colDanger
)

// withAlpha returns c with its alpha replaced (for tinted backgrounds).
func withAlpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// --- typography -----------------------------------------------------------

// h1 is a large bold page/brand title (explicit color so it works on any surface).
func h1(text string) *canvas.Text {
	t := canvas.NewText(text, colText)
	t.TextSize = 24
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// h2 is a section/card heading.
func h2(text string) *canvas.Text {
	t := canvas.NewText(text, colText)
	t.TextSize = 17
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// caption is small muted helper text (form labels, hints).
func caption(text string) *canvas.Text {
	t := canvas.NewText(text, colMuted)
	t.TextSize = 12
	return t
}

// captionWrap is multi-line muted helper text that WRAPS to the container width — for longer
// hints/warnings that would overflow the single-line canvas-text caption.
func captionWrap(text string) fyne.CanvasObject {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	l.Importance = widget.LowImportance
	return l
}

// sectionTitle is an uppercase muted eyebrow label above a group of controls.
func sectionTitle(text string) fyne.CanvasObject {
	t := canvas.NewText(strings.ToUpper(text), colMuted)
	t.TextSize = 11
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// --- surfaces -------------------------------------------------------------

// card wraps content on a white rounded surface with a hairline border and padding.
func card(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colSurface)
	bg.CornerRadius = 12
	bg.StrokeColor = colBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.NewPadded(content))
}

// panel is a filled rounded surface in an arbitrary tint (no border) — used for
// hero/side accents.
func panel(fill color.Color, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = 12
	return container.NewStack(bg, container.NewPadded(content))
}

// --- buttons --------------------------------------------------------------

// primaryButton is the filled accent call-to-action.
func primaryButton(label string, icon fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, tapped)
	b.Importance = widget.HighImportance
	return b
}

// dangerButton is the filled destructive action.
func dangerButton(label string, icon fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, tapped)
	b.Importance = widget.DangerImportance
	return b
}

// subtleButton is a quiet, low-emphasis action.
func subtleButton(label string, icon fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, tapped)
	b.Importance = widget.LowImportance
	return b
}

// --- pills / status -------------------------------------------------------

// pill is a compact colored status badge (e.g. online / offline / success).
func pill(text string, c color.NRGBA) fyne.CanvasObject {
	bg := canvas.NewRectangle(withAlpha(c, 0x26))
	bg.CornerRadius = 9
	label := canvas.NewText(text, c)
	label.TextSize = 12
	label.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewStack(bg, container.NewPadded(container.NewCenter(label)))
}

// --- empty / placeholder --------------------------------------------------

// emptyState renders a centered icon over a muted message for an empty pane.
func emptyState(icon fyne.Resource, msg string) fyne.CanvasObject {
	glyph := widget.NewIcon(icon)
	box := container.NewGridWrap(fyne.NewSize(44, 44), glyph)
	m := canvas.NewText(msg, colMuted)
	m.Alignment = fyne.TextAlignCenter
	m.TextSize = 14
	return container.NewCenter(container.NewVBox(container.NewCenter(box), container.NewCenter(m)))
}

// spinnerBox is a centered loading indicator to show while a list/modal fetches, instead
// of a blank pane that suddenly fills in.
func spinnerBox() fyne.CanvasObject {
	a := widget.NewActivity()
	a.Start()
	return container.NewPadded(container.NewCenter(a))
}

// indented left-pads content by px — used to nest sub-items under a group header.
func indented(o fyne.CanvasObject, px float32) fyne.CanvasObject {
	sp := canvas.NewRectangle(color.Transparent)
	sp.SetMinSize(fyne.NewSize(px, 0))
	return container.NewBorder(nil, nil, sp, nil, o)
}

// --- forms ----------------------------------------------------------------

// field stacks a caption label above an input control.
func field(label string, w fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(caption(label), w)
}

// --- brand / hero ---------------------------------------------------------

// logo returns the brand mark sized to a square edge.
func logo(size float32) *canvas.Image {
	img := canvas.NewImageFromResource(appIcon)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(size, size))
	return img
}

// --- read-only info -------------------------------------------------------

// kv renders a label/value pair in an aligned two-column row (for read-only detail).
func kv(key, value string) fyne.CanvasObject {
	k := canvas.NewText(key, colMuted)
	k.TextSize = 13
	return container.New(layout.NewFormLayout(), k, widget.NewLabel(orDash(value)))
}

// statusPill renders an online/offline badge.
func statusPill(online bool) fyne.CanvasObject {
	if online {
		return pill("Online", colSuccess)
	}
	return pill("Offline", colMuted)
}

// pageHeader is a detail-pane header: a title (+ optional subtitle) on the left and
// optional trailing action objects hugging the right edge.
func pageHeader(title, subtitle string, trailing ...fyne.CanvasObject) fyne.CanvasObject {
	var left fyne.CanvasObject = h2(title)
	if subtitle != "" {
		left = container.NewVBox(h2(title), caption(subtitle))
	}
	if len(trailing) == 0 {
		return left
	}
	return container.NewBorder(nil, nil, nil, container.NewHBox(trailing...), left)
}

// --- modals ---------------------------------------------------------------

// openForm shows a modal form dialog (auto-sized) with Confirm/Cancel buttons.
// onConfirm runs only when the user confirms. Use it for focused actions instead of
// stacking inline forms on a screen.
func openForm(win fyne.Window, title string, items []*widget.FormItem, onConfirm func()) {
	dialog.ShowForm(title, "Confirm", "Cancel", items, func(ok bool) {
		if ok && onConfirm != nil {
			onConfirm()
		}
	}, win)
}

// openView shows arbitrary content in a roomy, dismissable modal (for lists/results).
func openView(win fyne.Window, title string, content fyne.CanvasObject) {
	d := dialog.NewCustom(title, "Close", content, win)
	d.Resize(fyne.NewSize(560, 520))
	d.Show()
}

// --- menus / submenus -----------------------------------------------------

// popMenuUnder shows a popup menu anchored just below the given object.
func popMenuUnder(anchor fyne.CanvasObject, items ...*fyne.MenuItem) {
	drv := fyne.CurrentApp().Driver()
	c := drv.CanvasForObject(anchor)
	if c == nil {
		return
	}
	pop := widget.NewPopUpMenu(fyne.NewMenu("", items...), c)
	pos := drv.AbsolutePositionForObject(anchor)
	pop.ShowAtPosition(fyne.NewPos(pos.X, pos.Y+anchor.Size().Height))
}

// menuButton is a labeled button that opens a submenu of actions (e.g. "Add ▾").
func menuButton(label string, icon fyne.Resource, imp widget.Importance, items ...*fyne.MenuItem) *widget.Button {
	var b *widget.Button
	b = widget.NewButtonWithIcon(label, icon, func() { popMenuUnder(b, items...) })
	b.Importance = imp
	return b
}

// overflowButton is a quiet "⋯" button that opens an overflow menu (device-level /
// destructive actions live here rather than cluttering the screen).
func overflowButton(items ...*fyne.MenuItem) *widget.Button {
	var b *widget.Button
	b = widget.NewButtonWithIcon("", theme.MoreVerticalIcon(), func() { popMenuUnder(b, items...) })
	b.Importance = widget.LowImportance
	return b
}

// --- drill-in rows --------------------------------------------------------

// tappable makes any content clickable with a pointer cursor.
type tappable struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTappable(content fyne.CanvasObject, onTap func()) *tappable {
	t := &tappable{content: content, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappable) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(t.content) }
func (t *tappable) Tapped(_ *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}
func (t *tappable) Cursor() desktop.Cursor { return desktop.PointerCursor }

// navRow is a drill-in menu row (a tappable card): leading icon, title + subtitle, and
// a trailing chevron. Tapping opens the matching modal / sub-view. This is how a screen
// exposes a group of related actions without laying them all out flat.
func navRow(icon fyne.Resource, title, subtitle string, onTap func()) fyne.CanvasObject {
	ic := widget.NewIcon(icon)
	t := canvas.NewText(title, colText)
	t.TextStyle = fyne.TextStyle{Bold: true}
	t.TextSize = 14
	sub := canvas.NewText(subtitle, colMuted)
	sub.TextSize = 11
	chev := widget.NewIcon(theme.NavigateNextIcon())
	row := container.NewBorder(nil, nil, container.NewPadded(container.NewCenter(ic)), container.NewCenter(chev), container.NewVBox(t, sub))
	return newTappable(card(row), onTap)
}

// heroPane is the branded gradient panel used on the login screen.
func heroPane(tagline string) fyne.CanvasObject {
	grad := canvas.NewLinearGradient(colBrandLight, colBrandDark, 45)

	title := canvas.NewText("WeLock", color.White)
	title.TextSize = 36
	title.TextStyle = fyne.TextStyle{Bold: true}

	sub := canvas.NewText(tagline, color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xD0})
	sub.TextSize = 15

	inner := container.NewVBox(
		container.NewCenter(logo(104)),
		container.NewCenter(title),
		container.NewCenter(sub),
	)
	return container.NewStack(grad, container.NewCenter(inner))
}
