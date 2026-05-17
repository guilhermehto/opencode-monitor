package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

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
func TestRenderAllSessionsRecentSectionIsUnifiedAtBottom(t *testing.T) {
	// Two instances: live rows from each, plus one recent row in each.
	// The unified pane should render BOTH live rows above the single
	// "▾ N recent" header (sum across instances), then both recent rows
	// underneath when expanded.
	m := model{width: 200, snap: state.Snapshot{UpdatedAt: time.Unix(0, 0)}}

	liveA := liveSessionView("liveA-id", "", "busy", state.AttnActive)
	liveA.InstanceID = "instA"
	liveA.InstanceName = "instA"
	liveA.Title = "live-A-title"

	liveB := liveSessionView("liveB-id", "", "busy", state.AttnActive)
	liveB.InstanceID = "instB"
	liveB.InstanceName = "instB"
	liveB.Title = "live-B-title"

	recentA := recentSessionView("recentA-id", "", "idle", state.AttnInactive)
	recentA.InstanceID = "instA"
	recentA.InstanceName = "instA"
	recentA.Title = "recent-A-title"

	recentB := recentSessionView("recentB-id", "", "idle", state.AttnInactive)
	recentB.InstanceID = "instB"
	recentB.InstanceName = "instB"
	recentB.Title = "recent-B-title"

	rendered := m.renderAllSessions(200, []state.SessionView{liveA, liveB, recentA, recentB},
		map[string]int{"instA": 1, "instB": 1})

	liveAPos := strings.Index(rendered, "live-A-title")
	liveBPos := strings.Index(rendered, "live-B-title")
	markerPos := strings.Index(rendered, "2 recent")
	recentAPos := strings.Index(rendered, "recent-A-title")
	recentBPos := strings.Index(rendered, "recent-B-title")

	if liveAPos < 0 || liveBPos < 0 || markerPos < 0 || recentAPos < 0 || recentBPos < 0 {
		t.Fatalf("missing fragment in rendered output: %q", rendered)
	}
	// Every live row must precede the unified marker, and every recent
	// row must follow it. Order within live/recent groups is governed
	// by renderTree (covered elsewhere); here we only assert the
	// section-level placement.
	if liveAPos > markerPos || liveBPos > markerPos {
		t.Fatalf("expected live rows before recent marker; live-A=%d live-B=%d marker=%d", liveAPos, liveBPos, markerPos)
	}
	if recentAPos < markerPos || recentBPos < markerPos {
		t.Fatalf("expected recent rows after recent marker; recent-A=%d recent-B=%d marker=%d", recentAPos, recentBPos, markerPos)
	}
	// Marker count must sum recents across instances, not display per-instance counts.
	// (Use the dimmed "▾ 2 recent" / "▸ 2 recent" form so titles like
	// "recent-A-title" don't false-match.)
	if strings.Count(rendered, "▾ 2 recent") + strings.Count(rendered, "▸ 2 recent") != 1 {
		t.Fatalf("expected exactly one unified recent marker line, got rendered=%q", rendered)
	}
}

// cellOffsetAfter returns the visible cell offset just past the first
// occurrence of substr in s, or -1 if substr is not found. ANSI escape
// sequences in s contribute zero visible width.
func cellOffsetAfter(s, substr string) int {
	idx := strings.Index(s, substr)
	if idx < 0 {
		return -1
	}
	return lipgloss.Width(s[:idx+len(substr)])
}

// TestColumnHeaderAndRowAgreeOnWidth pins the invariant that the
// header and every data row produce exactly `width` visible cells.
// When this holds, header labels and row cells are positionally
// coupled — fix the columns once and the alignment follows for free.
func TestColumnHeaderAndRowAgreeOnWidth(t *testing.T) {
	sv := state.SessionView{
		SessionID:    "s1",
		Title:        "some title",
		Agent:        "scribe",
		StatusType:   "busy",
		Attention:    state.AttnActive,
		Source:       state.SourceLive,
		LastActivity: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	}
	now := time.Date(2026, 5, 15, 12, 5, 0, 0, time.UTC)
	for _, w := range []int{60, 80, 120, 200} {
		header := columnHeader(w)
		row := formatRow(now, sv, w, false)
		if hw := lipgloss.Width(header); hw != w {
			t.Errorf("width %d: header visible width = %d, want %d (%q)", w, hw, w, header)
		}
		if rw := lipgloss.Width(row); rw != w {
			t.Errorf("width %d: row visible width = %d, want %d (%q)", w, rw, w, row)
		}
	}
}

// TestFormatRowStatusUnderStatusHeader is the regression test for the
// original bug: with both status and activity present, "busy" must
// land under the STATUS header, not under ACTIVITY. Both are
// right-aligned inside the 10-cell STATUS column, so their trailing
// cell offsets must coincide.
func TestFormatRowStatusUnderStatusHeader(t *testing.T) {
	width := 120
	header := columnHeader(width)
	row := formatRow(time.Now(), state.SessionView{
		SessionID:  "s1",
		Title:      "alpha",
		StatusType: "busy",
		Attention:  state.AttnActive,
		Source:     state.SourceLive,
	}, width, false)

	statusEnd := cellOffsetAfter(header, "STATUS")
	busyEnd := cellOffsetAfter(row, "busy")
	if statusEnd < 0 || busyEnd < 0 {
		t.Fatalf("missing fragments: STATUS=%d busy=%d\nheader=%q\nrow=%q",
			statusEnd, busyEnd, header, row)
	}
	if statusEnd != busyEnd {
		t.Errorf("STATUS ends at cell %d, busy ends at cell %d; both should share the STATUS column's right edge\nheader=%q\nrow=%q",
			statusEnd, busyEnd, header, row)
	}
}

// TestShortenDirectory pins the rules for the CWD suffix: $HOME
// becomes "~", a path under $HOME becomes "~/<rest>", anything else
// is returned unchanged, and an empty input round-trips to "" so
// callers can append unconditionally.
func TestShortenDirectory(t *testing.T) {
	prev := homeDir
	defer func() { homeDir = prev }()
	homeDir = "/home/me"

	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"/home/me", "~"},
		{"/home/me/src/foo", "~/src/foo"},
		{"/home/me/src/foo/bar/baz", "~/src/foo/bar/baz"},
		{"/tmp/work", "/tmp/work"},
		// Prefix-only match (no trailing slash + path) must NOT
		// rewrite — "/home/melissa" is a different user, not a
		// subpath of "/home/me".
		{"/home/melissa/src", "/home/melissa/src"},
	}
	for _, c := range cases {
		if got := shortenDirectory(c.in); got != c.want {
			t.Errorf("shortenDirectory(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortenDirectoryNoHome(t *testing.T) {
	prev := homeDir
	defer func() { homeDir = prev }()
	homeDir = ""
	// With $HOME unresolved we must not substitute anything; the
	// path is returned verbatim so the display still identifies
	// the session's project.
	if got := shortenDirectory("/Users/g/src/foo"); got != "/Users/g/src/foo" {
		t.Errorf("shortenDirectory with empty homeDir = %q, want path unchanged", got)
	}
}

// TestFormatRowShowsShortenedDirectory pins the actual rendering: a
// root row with a non-empty Directory appends the home-relative form
// after the title, dim-styled. We assert on the visible substring
// (lipgloss escape codes are wrapped around the literal text, not
// interleaved with it).
func TestFormatRowShowsShortenedDirectory(t *testing.T) {
	prev := homeDir
	defer func() { homeDir = prev }()
	homeDir = "/home/me"

	sv := state.SessionView{
		SessionID:  "s1",
		Title:      "alpha",
		Directory:  "/home/me/src/foo",
		StatusType: "busy",
		Attention:  state.AttnActive,
		Source:     state.SourceLive,
	}
	row := formatRow(time.Now(), sv, 120, false)
	if !strings.Contains(row, "~/src/foo") {
		t.Fatalf("row = %q, want shortened directory ~/src/foo", row)
	}
	// Raw path must not leak alongside the shortened form.
	if strings.Contains(row, "/home/me/src/foo") {
		t.Fatalf("row = %q, want raw $HOME path replaced", row)
	}
}

// TestFormatRowOmitsDirectoryOnChild pins the "subagents inherit the
// parent's cwd, so don't repeat it on the ↳ row" rule. If this ever
// needs to flip to "show only when different from parent", that's a
// renderTree-level change because the parent's directory has to flow
// in alongside the child row.
func TestFormatRowOmitsDirectoryOnChild(t *testing.T) {
	prev := homeDir
	defer func() { homeDir = prev }()
	homeDir = "/home/me"

	sv := state.SessionView{
		SessionID:  "child",
		ParentID:   "root",
		Title:      "child-work",
		Directory:  "/home/me/src/foo",
		Agent:      "scribe",
		StatusType: "busy",
		Attention:  state.AttnActive,
		Source:     state.SourceLive,
	}
	row := formatRow(time.Now(), sv, 120, true)
	if strings.Contains(row, "~/src/foo") || strings.Contains(row, "/home/me/src/foo") {
		t.Fatalf("child row = %q, must not render directory", row)
	}
}

// TestFormatRowActivityUnderActivityHeader pins the same invariant
// for the ACTIVITY column.
func TestFormatRowActivityUnderActivityHeader(t *testing.T) {
	width := 120
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	header := columnHeader(width)
	row := formatRow(now, state.SessionView{
		SessionID:    "s1",
		Title:        "alpha",
		Attention:    state.AttnInactive,
		Source:       state.SourceLive,
		LastActivity: now.Add(-5 * time.Minute),
	}, width, false)

	activityEnd := cellOffsetAfter(header, "ACTIVITY")
	valueEnd := cellOffsetAfter(row, "5m")
	if activityEnd < 0 || valueEnd < 0 {
		t.Fatalf("missing fragments: ACTIVITY=%d value=%d\nheader=%q\nrow=%q",
			activityEnd, valueEnd, header, row)
	}
	if activityEnd != valueEnd {
		t.Errorf("ACTIVITY ends at cell %d, 5m ends at cell %d\nheader=%q\nrow=%q",
			activityEnd, valueEnd, header, row)
	}
}
