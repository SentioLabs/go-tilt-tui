// Package overlay holds the modal panels drawn above the two panes.
package overlay

import tea "charm.land/bubbletea/v2"

// Overlay is one panel on the overlay stack. Only the top one gets keys.
type Overlay interface {
	Update(tea.Msg) (Overlay, tea.Cmd)
	View(width, height int) string
	Title() string
}

// CloseMsg asks the root model to pop the top overlay.
type CloseMsg struct{}
