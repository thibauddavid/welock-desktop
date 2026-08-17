package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// DefaultWidth / DefaultHeight are the desktop-appropriate starting window size.
const (
	DefaultWidth  = 1120
	DefaultHeight = 760
)

// welockTheme is the app's design-system theme. It pins a single light appearance
// (so the app looks the same regardless of the OS setting), maps the Fyne color
// roles onto the WeLock palette in kit.go, and tunes spacing/typography/radii for a
// roomier, more modern desktop feel. All concrete colors live in kit.go so screens
// and the theme share one source of truth.
type welockTheme struct{}

// NewTheme returns the application's theme.
func NewTheme() fyne.Theme { return &welockTheme{} }

func (t *welockTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colBg
	case theme.ColorNameForeground:
		return colText
	case theme.ColorNameForegroundOnPrimary:
		return color.White
	case theme.ColorNamePrimary:
		return colBrand
	case theme.ColorNameHover:
		return withAlpha(colBrand, 0x1E)
	case theme.ColorNamePressed:
		return withAlpha(colBrand, 0x30)
	case theme.ColorNameFocus:
		return colBrand
	case theme.ColorNameSelection:
		return withAlpha(colBrand, 0x22)
	case theme.ColorNameButton:
		return colSurface
	case theme.ColorNameInputBackground:
		return colSurface
	case theme.ColorNameInputBorder:
		return colBorder
	case theme.ColorNameSeparator:
		return colBorder
	case theme.ColorNamePlaceHolder:
		return colMuted
	case theme.ColorNameDisabled:
		return withAlpha(colMuted, 0x88)
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return colSurface
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0x10, G: 0x18, B: 0x20, A: 0x1A}
	case theme.ColorNameSuccess:
		return colSuccess
	case theme.ColorNameWarning:
		return colWarning
	case theme.ColorNameError:
		return colDanger
	}
	return theme.DefaultTheme().Color(name, theme.VariantLight)
}

func (t *welockTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *welockTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *welockTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 24
	case theme.SizeNameSubHeadingText:
		return 17
	case theme.SizeNameCaptionText:
		return 12
	case theme.SizeNameInputRadius:
		return 9
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBar:
		return 12
	case theme.SizeNameScrollBarSmall:
		return 4
	}
	return theme.DefaultTheme().Size(name)
}
