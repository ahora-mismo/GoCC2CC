package ui

import "github.com/charmbracelet/lipgloss"

// Every style lives here. Scattered across the screen files they drift out of
// agreement within a few edits — the tabs stop matching the footer, one list
// pads differently from the other.
var (
	colDim      = lipgloss.AdaptiveColor{Light: "245", Dark: "241"}
	colFaint    = lipgloss.AdaptiveColor{Light: "250", Dark: "238"}
	colText     = lipgloss.AdaptiveColor{Light: "236", Dark: "252"}
	colAccent   = lipgloss.AdaptiveColor{Light: "31", Dark: "38"}
	colWarn     = lipgloss.AdaptiveColor{Light: "166", Dark: "209"}
	colSystem   = lipgloss.AdaptiveColor{Light: "97", Dark: "141"}
	colSelected = lipgloss.AdaptiveColor{Light: "254", Dark: "237"}

	styleTabActive = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true).
			Padding(0, 2)

	styleTabInactive = lipgloss.NewStyle().
				Foreground(colDim).
				Padding(0, 2)

	styleTabBar = lipgloss.NewStyle().
			Padding(0, 1)

	styleRule = lipgloss.NewStyle().
			Foreground(colFaint)

	styleHeader = lipgloss.NewStyle().
			Foreground(colDim).
			Padding(0, 2)

	styleSubhead = lipgloss.NewStyle().
			Foreground(colDim).
			Italic(true).
			Padding(0, 2)

	styleRow = lipgloss.NewStyle().
			Foreground(colText).
			Padding(0, 2)

	styleRowSelected = lipgloss.NewStyle().
				Foreground(colText).
				Background(colSelected).
				Padding(0, 2)

	styleCursor = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	styleMuted = lipgloss.NewStyle().
			Foreground(colDim).
			Padding(0, 2)

	styleSystem = lipgloss.NewStyle().
			Foreground(colSystem)

	styleFooter = lipgloss.NewStyle().
			Foreground(colDim).
			Padding(0, 2)

	styleFooterKey = lipgloss.NewStyle().
			Foreground(colText).
			Bold(true)

	styleConfirm = lipgloss.NewStyle().
			Foreground(colWarn).
			Bold(true).
			Padding(0, 2)

	styleFieldName = lipgloss.NewStyle().
			Foreground(colDim)

	styleNotice = lipgloss.NewStyle().
			Foreground(colWarn).
			Padding(0, 2)

	styleEmpty = lipgloss.NewStyle().
			Foreground(colDim).
			Italic(true).
			Padding(1, 2)
)
