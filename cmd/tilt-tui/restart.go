package main

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui"
)

func init() { upOptions = append(upOptions, restartOption) }

// restartOption installs restartFilter as the program's message filter.
// Bubble Tea keeps one filter, so no other file may add one.
func restartOption(s *supervisor) tea.ProgramOption {
	return tea.WithFilter(restartFilter(s))
}

// restartFilter takes ui.RestartRequestMsg out of the message stream and
// starts s.restart in a goroutine. The filter runs on the event loop, and
// a restart waits out tilt's startup window, so it must not block. Every
// other message passes through unchanged.
func restartFilter(s *supervisor) func(tea.Model, tea.Msg) tea.Msg {
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(ui.RestartRequestMsg); !ok {
			return msg
		}
		go s.restart()
		return nil
	}
}

// restart launches tilt again with the original port and arguments. The
// supervisor then sends ui.ChildStartedMsg and resumes the reconnect
// loop. If tilt exits within its startup window, the UI gets a
// ui.ChildExitedMsg holding the error and tilt's stderr, so R works
// again. A restart while a child runs or another launch is in flight does
// nothing.
func (s *supervisor) restart() {
	if err := s.launch(); err != nil {
		s.send(ui.ChildExitedMsg{Code: exitCode(err), Stderr: err.Error()})
	}
}
