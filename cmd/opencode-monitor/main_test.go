package main

import (
	"strings"
	"testing"
	"time"

	"github.com/goliveira/opencode-monitor/internal/state"
)

func makeSessionView(id, parentID, status string, attn state.Attention) state.SessionView {
	return state.SessionView{
		InstanceID:   "inst-1",
		InstanceName: "inst-1",
		SessionID:    id,
		ParentID:     parentID,
		StatusType:   status,
		Attention:    attn,
		Source:       state.SourceLive,
	}
}

func liveSessionView(id, parentID, status string, attn state.Attention) state.SessionView {
	sv := makeSessionView(id, parentID, status, attn)
	sv.Source = state.SourceLive
	return sv
}

func recentSessionView(id, parentID, status string, attn state.Attention) state.SessionView {
	sv := makeSessionView(id, parentID, status, attn)
	sv.Source = state.SourceRecent
	return sv
}

func TestVisibleSessionsHidesFinishedSubagents(t *testing.T) {
	rows := []state.SessionView{
		makeSessionView("root", "", "idle", state.AttnInactive),
		makeSessionView("child-idle", "root", "idle", state.AttnInactive),
		makeSessionView("child-busy", "root", "busy", state.AttnActive),
	}

	visible, _ := visibleSessions(rows, false)
	if len(visible) != 2 {
		t.Fatalf("visible count = %d, want 2", len(visible))
	}

	ids := map[string]state.SessionView{}
	for _, sv := range visible {
		ids[sv.SessionID] = sv
	}
	if _, ok := ids["child-idle"]; ok {
		t.Fatalf("idle subagent should be hidden")
	}
	if got := ids["child-busy"].ParentID; got != "root" {
		t.Fatalf("busy subagent parent = %q, want root", got)
	}
}

func TestVisibleSessionsKeepsUrgentSubagentsVisible(t *testing.T) {
	rows := []state.SessionView{
		makeSessionView("root", "", "idle", state.AttnInactive),
		makeSessionView("child-perm", "root", "idle", state.AttnPermissionPending),
		makeSessionView("child-question", "root", "idle", state.AttnQuestionPending),
		makeSessionView("child-err", "root", "", state.AttnErrored),
	}

	visible, _ := visibleSessions(rows, false)
	ids := map[string]bool{}
	for _, sv := range visible {
		ids[sv.SessionID] = true
	}
	if !ids["child-perm"] || !ids["child-question"] || !ids["child-err"] {
		t.Fatalf("urgent subagents must stay visible: %+v", ids)
	}
}

func TestVisibleSessionsReparentsAcrossHiddenAncestor(t *testing.T) {
	rows := []state.SessionView{
		makeSessionView("root", "", "idle", state.AttnInactive),
		makeSessionView("mid-idle", "root", "idle", state.AttnInactive),
		makeSessionView("leaf-busy", "mid-idle", "busy", state.AttnActive),
	}

	visible, _ := visibleSessions(rows, false)
	for _, sv := range visible {
		if sv.SessionID == "leaf-busy" {
			if sv.ParentID != "root" {
				t.Fatalf("leaf parent = %q, want root", sv.ParentID)
			}
			return
		}
	}
	t.Fatalf("leaf-busy row not visible")
}

func TestFormatRowAgentBeforeTitleAndDedupesAgentSuffix(t *testing.T) {
	sv := state.SessionView{
		SessionID:  "s1",
		Title:      "refactor parser (scribe)",
		Agent:      "scribe",
		StatusType: "busy",
		Attention:  state.AttnActive,
		Source:     state.SourceLive,
	}

	row := formatRow(time.Now(), sv, 120, false)
	if !strings.Contains(row, "@scribe refactor parser") {
		t.Fatalf("row = %q, want agent before title", row)
	}
	if strings.Contains(row, "(scribe)") {
		t.Fatalf("row = %q, want duplicate agent suffix removed", row)
	}
}

func TestRenderAllSessionsRedactsInstanceHostPort(t *testing.T) {
	m := model{}
	rows := []state.SessionView{
		{
			InstanceID:   "a",
			InstanceName: "10.0.0.1:1234",
			SessionID:    "s1",
			Title:        "alpha",
			StatusType:   "idle",
			Attention:    state.AttnInactive,
			Source:       state.SourceLive,
		},
		{
			InstanceID:   "b",
			InstanceName: "10.0.0.2:5678",
			SessionID:    "s2",
			Title:        "beta",
			StatusType:   "busy",
			Attention:    state.AttnActive,
			Source:       state.SourceLive,
		},
	}

	rendered := m.renderAllSessions(120, rows, nil)
	if strings.Contains(rendered, "Instance 1") || strings.Contains(rendered, "Instance 2") {
		t.Fatalf("rendered = %q, want instance labels removed", rendered)
	}
	if strings.Contains(rendered, "10.0.0.1:1234") || strings.Contains(rendered, "10.0.0.2:5678") {
		t.Fatalf("rendered = %q, want host:port redacted", rendered)
	}
}

func TestViewDoesNotRenderNeedsAttentionPane(t *testing.T) {
	m := model{
		width: 120,
		snap: state.Snapshot{
			UpdatedAt: time.Unix(0, 0),
			Sessions: []state.SessionView{
				{
					InstanceID:   "a",
					InstanceName: "inst-a",
					SessionID:    "s1",
					Title:        "alpha",
					StatusType:   "busy",
					Attention:    state.AttnPermissionPending,
					Source:       state.SourceLive,
				},
			},
		},
	}

	rendered := m.View()
	if strings.Contains(rendered, "Needs attention") {
		t.Fatalf("rendered = %q, want no needs-attention pane", rendered)
	}
	if !strings.Contains(rendered, "Sessions") {
		t.Fatalf("rendered = %q, want sessions pane", rendered)
	}
}

func TestFormatRelativeBoundaries(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero time renders empty", time.Time{}, ""},
		{"now (0 diff)", now, "now"},
		{"30s under a minute", now.Add(-30 * time.Second), "now"},
		{"exactly 1m", now.Add(-time.Minute), "1m"},
		{"59m just under an hour", now.Add(-59 * time.Minute), "59m"},
		{"exactly 60m flips to 1h", now.Add(-60 * time.Minute), "1h"},
		{"24h", now.Add(-24 * time.Hour), "24h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatRelative(now, c.t)
			if got != c.want {
				t.Fatalf("formatRelative(%v) = %q, want %q", c.t, got, c.want)
			}
		})
	}
}

func TestVisibleSessionsCollapseDropsRecentKeepsLive(t *testing.T) {
	rows := []state.SessionView{
		// Live rows of every flavour, including inactive — none should be
		// dropped by the collapse since the user wants to see what the
		// monitor is currently observing, idle or not.
		liveSessionView("calm", "", "idle", state.AttnInactive),
		liveSessionView("urgent", "", "idle", state.AttnPermissionPending),
		liveSessionView("question", "", "idle", state.AttnQuestionPending),
		liveSessionView("errored", "", "", state.AttnErrored),
		liveSessionView("active", "", "busy", state.AttnActive),
		// A historical row imported from /session — this is the only one
		// the collapse should drop.
		recentSessionView("history", "", "idle", state.AttnInactive),
	}

	visible, counts := visibleSessions(rows, true)
	ids := map[string]bool{}
	for _, sv := range visible {
		ids[sv.SessionID] = true
	}
	for _, must := range []string{"calm", "urgent", "question", "errored", "active"} {
		if !ids[must] {
			t.Fatalf("collapsed view must keep live row %q visible: %+v", must, ids)
		}
	}
	if ids["history"] {
		t.Fatalf("collapsed view must drop recent row: %+v", ids)
	}
	if counts["inst-1"] != 1 {
		t.Fatalf("recent count for inst-1 = %d, want 1", counts["inst-1"])
	}
}

func TestVisibleSessionsExpandKeepsRecent(t *testing.T) {
	rows := []state.SessionView{
		liveSessionView("active", "", "busy", state.AttnActive),
		recentSessionView("history", "", "idle", state.AttnInactive),
	}
	visible, counts := visibleSessions(rows, false)
	ids := map[string]bool{}
	for _, sv := range visible {
		ids[sv.SessionID] = true
	}
	if !ids["history"] {
		t.Fatalf("expanded view must keep recent row visible: %+v", ids)
	}
	if counts["inst-1"] != 1 {
		t.Fatalf("recent count for inst-1 = %d, want 1", counts["inst-1"])
	}
}

func TestVisibleSessionsCollapseReparentsLiveChildAcrossHiddenRecentRoot(t *testing.T) {
	rows := []state.SessionView{
		recentSessionView("recent-root", "", "idle", state.AttnInactive),
		liveSessionView("live-child", "recent-root", "busy", state.AttnActive),
	}
	visible, _ := visibleSessions(rows, true)
	for _, sv := range visible {
		if sv.SessionID == "live-child" {
			if sv.ParentID != "" {
				t.Fatalf("live child of hidden recent root should be promoted to root, got parent %q", sv.ParentID)
			}
			return
		}
	}
	t.Fatalf("live-child not visible: %+v", visible)
}

func TestProcessBellTransitionsFiresOnceWhileAttentionStable(t *testing.T) {
	bellSent := map[rowKey]state.Attention{}
	row := state.SessionView{
		InstanceID: "inst-1",
		SessionID:  "s1",
		Attention:  state.AttnPermissionPending,
	}

	first := processBellTransitions([]state.SessionView{row}, bellSent)
	if len(first) != 1 {
		t.Fatalf("first snapshot fired %d bells, want 1", len(first))
	}
	second := processBellTransitions([]state.SessionView{row}, bellSent)
	if len(second) != 0 {
		t.Fatalf("second snapshot in same attention fired %d bells, want 0", len(second))
	}
	third := processBellTransitions([]state.SessionView{row}, bellSent)
	if len(third) != 0 {
		t.Fatalf("third snapshot in same attention fired %d bells, want 0", len(third))
	}
}

func TestProcessBellTransitionsFiresAgainAfterLeavingAttention(t *testing.T) {
	bellSent := map[rowKey]state.Attention{}
	pending := state.SessionView{
		InstanceID: "inst-1",
		SessionID:  "s1",
		Attention:  state.AttnPermissionPending,
	}
	calm := pending
	calm.Attention = state.AttnInactive

	if got := processBellTransitions([]state.SessionView{pending}, bellSent); len(got) != 1 {
		t.Fatalf("entry into attention fired %d bells, want 1", len(got))
	}
	if got := processBellTransitions([]state.SessionView{calm}, bellSent); len(got) != 0 {
		t.Fatalf("leaving attention fired %d bells, want 0", len(got))
	}
	if _, ok := bellSent[rowKey{instanceID: "inst-1", sessionID: "s1"}]; ok {
		t.Fatalf("bellSent must clear once a session leaves attention")
	}
	if got := processBellTransitions([]state.SessionView{pending}, bellSent); len(got) != 1 {
		t.Fatalf("re-entry into attention fired %d bells, want 1", len(got))
	}
}

func TestProcessBellTransitionsFiresOnAttentionTypeChange(t *testing.T) {
	bellSent := map[rowKey]state.Attention{}
	perm := state.SessionView{
		InstanceID: "inst-1",
		SessionID:  "s1",
		Attention:  state.AttnPermissionPending,
	}
	errored := perm
	errored.Attention = state.AttnErrored

	if got := processBellTransitions([]state.SessionView{perm}, bellSent); len(got) != 1 {
		t.Fatalf("first attention fired %d bells, want 1", len(got))
	}
	if got := processBellTransitions([]state.SessionView{errored}, bellSent); len(got) != 1 {
		t.Fatalf("attention type change fired %d bells, want 1", len(got))
	}
}

func TestProcessBellTransitionsPrunesDisappearedSessions(t *testing.T) {
	bellSent := map[rowKey]state.Attention{}
	row := state.SessionView{
		InstanceID: "inst-1",
		SessionID:  "s1",
		Attention:  state.AttnPermissionPending,
	}
	processBellTransitions([]state.SessionView{row}, bellSent)
	if len(bellSent) != 1 {
		t.Fatalf("bellSent len = %d, want 1", len(bellSent))
	}
	processBellTransitions(nil, bellSent)
	if len(bellSent) != 0 {
		t.Fatalf("disappeared session should be pruned, bellSent = %+v", bellSent)
	}
}

func TestRenderAllSessionsRecentMarkerSitsAboveRecentRows(t *testing.T) {
	m := model{width: 200, snap: state.Snapshot{UpdatedAt: time.Unix(0, 0)}}
	live := liveSessionView("live-row", "", "idle", state.AttnInactive)
	live.Title = "live-title"
	recent := recentSessionView("recent-row", "", "idle", state.AttnInactive)
	recent.Title = "recent-title"

	rendered := m.renderAllSessions(200, []state.SessionView{live, recent}, map[string]int{"inst-1": 1})

	livePos := strings.Index(rendered, "live-title")
	markerPos := strings.Index(rendered, "1 recent")
	recentPos := strings.Index(rendered, "recent-title")
	if livePos < 0 || markerPos < 0 || recentPos < 0 {
		t.Fatalf("missing fragment in rendered output: %q", rendered)
	}
	if !(livePos < markerPos && markerPos < recentPos) {
		t.Fatalf("expected order live -> marker -> recent, got positions live=%d marker=%d recent=%d in %q",
			livePos, markerPos, recentPos, rendered)
	}
}

func TestRenderAllSessionsCollapsedHidesRecentRowsButKeepsMarker(t *testing.T) {
	m := model{width: 200, recentCollapsed: true, snap: state.Snapshot{UpdatedAt: time.Unix(0, 0)}}
	// visibleSessions(collapse=true) is the source of truth for which
	// rows reach the renderer; recreate that contract here.
	visible, counts := visibleSessions([]state.SessionView{
		liveSessionView("live", "", "busy", state.AttnActive),
		recentSessionView("history", "", "idle", state.AttnInactive),
	}, true)

	rendered := m.renderAllSessions(200, visible, counts)
	if strings.Contains(rendered, "history") {
		t.Fatalf("collapsed view must not render recent row title, got %q", rendered)
	}
	if !strings.Contains(rendered, "1 recent") {
		t.Fatalf("collapsed view must still show recent count marker, got %q", rendered)
	}
}
