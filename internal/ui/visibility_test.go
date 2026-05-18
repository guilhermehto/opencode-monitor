package ui

import (
	"testing"

	"github.com/guilhermehto/cogitator/internal/state"
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

func TestVisibleSessionsCollapseDropsRecentKeepsLive(t *testing.T) {
	rows := []state.SessionView{
		liveSessionView("calm", "", "idle", state.AttnInactive),
		liveSessionView("urgent", "", "idle", state.AttnPermissionPending),
		liveSessionView("question", "", "idle", state.AttnQuestionPending),
		liveSessionView("errored", "", "", state.AttnErrored),
		liveSessionView("active", "", "busy", state.AttnActive),
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
