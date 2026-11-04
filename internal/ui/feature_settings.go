package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// settingsFeature opens the settings pane on ",". The pane only reports
// changes as overlay.SettingMsg, and the feature that owns each setting
// applies it for the session. Nothing writes the config file.
var settingsFeature = Feature{
	Name: "settings",
	Commands: func(m *Model) []command.Command {
		return []command.Command{{
			ID: "settings", Title: "settings", Keys: []string{","}, Scope: command.ScopeGlobal,
			Run: func(command.Context) tea.Cmd {
				return msgCmd(PushOverlayMsg{Overlay: overlay.NewSettings(m.d.Theme, []overlay.Setting{wheelSetting(m)})})
			},
		}}
	},
}

func init() { register(settingsFeature) }
