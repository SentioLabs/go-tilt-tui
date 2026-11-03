// Package command is the single list of user actions. Key handling, the
// help screen and the command palette all read from it.
package command

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

type ID string

// Scope says where a command's keys are active.
type Scope int

const (
	ScopeGlobal Scope = iota
	ScopeList
	ScopeLogs
)

type Focus int

const (
	FocusList Focus = iota
	FocusLogs
)

// Context is what a command can see when deciding if it applies.
type Context struct {
	Focus    Focus
	Resource *tilt.Resource // nil when "All Resources" is selected
	Narrow   bool
}

type Command struct {
	ID      ID
	Title   string   // shown in help and the palette
	Keys    []string // tea.KeyPressMsg.String() values
	Scope   Scope
	Enabled func(Context) bool // nil means always enabled
	Run     func(Context) tea.Cmd
}
