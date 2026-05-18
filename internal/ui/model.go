package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/guilhermehto/cogitator/internal/config"
	"github.com/guilhermehto/cogitator/internal/state"
)

type snapshotMsg state.Snapshot

type model struct {
	snap            state.Snapshot
	width           int
	height          int
	snaps           <-chan state.Snapshot
	recentCollapsed bool
	bellEnabled     bool
	bellSent        map[rowKey]state.Attention
	cfg             *config.Config
}

func (m model) Init() tea.Cmd {
	return waitSnapshot(m.snaps)
}

func waitSnapshot(ch <-chan state.Snapshot) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return nil
		}
		return snapshotMsg(s)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "a":
			m.recentCollapsed = !m.recentCollapsed
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case snapshotMsg:
		m.snap = state.Snapshot(msg)
		next := waitSnapshot(m.snaps)
		if !m.bellEnabled {
			return m, next
		}
		fired := processBellTransitions(m.snap.Sessions, m.bellSent)
		return m, tea.Batch(next, bellCmd(len(fired)))
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return "loading..."
	}

	rows, recentByInstance := visibleSessions(m.snap.Sessions, m.recentCollapsed)
	paneW := m.width - 2
	if paneW < 30 {
		paneW = 30
	}
	body := paneStyle.Width(paneW).Render(m.renderAllSessions(paneW, rows, recentByInstance))

	live, recent := 0, 0
	for _, sv := range rows {
		if sv.Source == state.SourceRecent {
			recent++
		} else {
			live++
		}
	}

	cfg := m.cfg
	if cfg == nil {
		cfg = config.Default()
	}
	recentMins := int(cfg.RecentWindow.Minutes())
	header := titleStyle.Render("cogitator") + dimStyle.Render(
		fmt.Sprintf("  %d live · %d recent (≤%dm)  ·  updated %s  ·  a to %s recent  ·  q to quit",
			live, recent, recentMins, m.snap.UpdatedAt.Format("15:04:05"), toggleVerb(m.recentCollapsed)),
	)

	legend := legendLine()
	footer := unreachableFooter(m.snap.UnreachableInstances)
	if footer == "" {
		return header + "\n" + body + "\n" + legend
	}
	return header + "\n" + body + "\n" + legend + "\n" + footer
}

func newModel(snaps <-chan state.Snapshot, cfg *config.Config, bellEnabled bool) model {
	if cfg == nil {
		cfg = config.Default()
	}
	return model{
		snaps:           snaps,
		recentCollapsed: true,
		bellEnabled:     bellEnabled,
		bellSent:        map[rowKey]state.Attention{},
		cfg:             cfg,
	}
}
