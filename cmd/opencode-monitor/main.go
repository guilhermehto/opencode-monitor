package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/goliveira/opencode-monitor/internal/discovery"
	"github.com/goliveira/opencode-monitor/internal/oc"
	"github.com/goliveira/opencode-monitor/internal/state"
)

const recentWindow = 30 * time.Minute

type instanceLifecycle struct {
	cancel context.CancelFunc
}

type supervisor struct {
	mu        sync.Mutex
	store     *state.Store
	instances map[string]*instanceLifecycle
	pollEvery time.Duration
}

func newSupervisor(store *state.Store) *supervisor {
	return &supervisor{
		store:     store,
		instances: map[string]*instanceLifecycle{},
		pollEvery: 5 * time.Second,
	}
}

func (s *supervisor) onAdd(parent context.Context, inst discovery.Instance) {
	s.mu.Lock()
	if _, exists := s.instances[inst.ID]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.instances[inst.ID] = &instanceLifecycle{cancel: cancel}
	s.mu.Unlock()
	s.store.AddInstance(inst)
	go s.run(ctx, inst)
}

func (s *supervisor) onRemove(id string) {
	s.mu.Lock()
	lc := s.instances[id]
	delete(s.instances, id)
	s.mu.Unlock()
	if lc != nil {
		lc.cancel()
	}
	s.store.RemoveInstance(id)
}

func (s *supervisor) run(ctx context.Context, inst discovery.Instance) {
	client := oc.NewClient(inst.BaseURL())

	syncPerms := func() {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if perms, err := client.PendingPermissions(pctx); err == nil {
			s.store.SyncPermissions(inst.ID, perms)
		}
	}
	syncRecent := func() {
		rctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if sessions, err := client.ListRecentSessions(rctx, recentWindow); err == nil {
			s.store.SyncRecent(inst.ID, sessions)
		}
	}

	syncPerms()
	syncRecent()

	// Permissions poll: fast, real-time signal.
	pTicker := time.NewTicker(s.pollEvery)
	defer pTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pTicker.C:
				syncPerms()
			}
		}
	}()
	// Recency poll: slower; just for discovery, not real-time.
	rTicker := time.NewTicker(30 * time.Second)
	defer rTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-rTicker.C:
				syncRecent()
			}
		}
	}()

	attempt := 0
	for ctx.Err() == nil {
		events := make(chan oc.Event, 32)
		done := make(chan error, 1)
		go func() { done <- client.SubscribeEvents(ctx, events) }()
	stream:
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-events:
				attempt = 0
				s.store.ApplyEvent(inst.ID, evt)
			case <-done:
				break stream
			}
		}
		attempt++
		oc.SleepBackoff(ctx, attempt)
	}
}

// ---- Bubble Tea ----

type snapshotMsg state.Snapshot

type model struct {
	snap            state.Snapshot
	width           int
	height          int
	snaps           <-chan state.Snapshot
	recentCollapsed bool
	bellEnabled     bool
	bellSent        map[rowKey]state.Attention
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
		if m.bellEnabled {
			fired := processBellTransitions(m.snap.Sessions, m.bellSent)
			for range fired {
				_, _ = os.Stderr.Write([]byte{'\a'})
			}
		}
		return m, waitSnapshot(m.snaps)
	}
	return m, nil
}

// processBellTransitions diffs the current snapshot against bellSent
// and returns the rowKeys that should ring the bell on this tick.
//
// A bell fires when a session transitions into an attention state for
// the first time, or transitions between two distinct attention
// states (e.g. PERMISSION -> ERROR). It does NOT fire on subsequent
// snapshots while the session sits in the same attention state. Once
// a session leaves the attention set, its bellSent entry is cleared
// so re-entering will fire again.
//
// bellSent is mutated in place; entries for sessions that disappeared
// from the snapshot are pruned so the map cannot grow unbounded.
func processBellTransitions(rows []state.SessionView, bellSent map[rowKey]state.Attention) []rowKey {
	seen := map[rowKey]bool{}
	var fired []rowKey
	for _, sv := range rows {
		key := rowKey{instanceID: sv.InstanceID, sessionID: sv.SessionID}
		seen[key] = true
		if needsAttention(sv.Attention) {
			if bellSent[key] != sv.Attention {
				fired = append(fired, key)
				bellSent[key] = sv.Attention
			}
		} else {
			delete(bellSent, key)
		}
	}
	for k := range bellSent {
		if !seen[k] {
			delete(bellSent, k)
		}
	}
	return fired
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Padding(0, 1)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	agentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Italic(true)
	recentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	paneStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	attnPermStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	attnQuestionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	attnErrStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	attnInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	attnActiveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))

	statusBusyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
)

// agentPalette is a curated set of 256-color codes that read well against
// a dark terminal background. The four attention colours (214/226/196/82)
// are intentionally excluded so a stable agent colour can never visually
// collide with PERMISSION/QUESTION/ERROR/active.
var agentPalette = []string{
	"33", "39", "45", "51", "75", "81",
	"99", "105", "111", "117", "135", "141",
	"147", "153", "165", "171", "177", "183",
	"203", "207", "213", "219",
}

// agentColor returns a stable italic style for a given agent name. The
// hash-mod indexing means the same agent always lands on the same
// colour across snapshots and across instances.
func agentColor(name string) lipgloss.Style {
	if name == "" {
		return agentStyle
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	idx := h.Sum32() % uint32(len(agentPalette))
	return lipgloss.NewStyle().Foreground(lipgloss.Color(agentPalette[idx])).Italic(true)
}

// Single-cell mdi glyphs that replace the padded state label. Each glyph
// is paired with the existing attention colour so an attention row visibly
// dominates an inactive one without reading the label.
const (
	glyphActive     = "\U000f09de" // 󰧞
	glyphInactive   = "\U000f0764" // 󰝤
	glyphRecent     = "\U000f02da" // 󰋚
	glyphQuestion   = "\U000f0625" // 󰘥
	glyphPermission = "\U000f033e" // 󰌾
	glyphError      = "\U000f0026" // 󰀦
)

// Column widths for the sessions table. STATE / STATUS / ACTIVITY are
// fixed; SESSION absorbs the remaining width inside the pane so the
// header labels and per-row values land in identical cells. Widths are
// chosen as max(header_label, max_value_width):
//
//	STATE    = max("STATE"(5), glyph+pad(2)) = 5
//	STATUS   = max("STATUS"(6), "generating"(10)) = 10
//	ACTIVITY = max("ACTIVITY"(8), "999h"(4)) = 8
const (
	colStateW    = 5
	colStatusW   = 10
	colActivityW = 8
	colGap       = 2
)

// attnLabel returns a single coloured glyph plus a trailing space so the
// STATE column occupies a stable 2-cell footprint regardless of state.
func attnLabel(a state.Attention, source state.Source) string {
	switch a {
	case state.AttnPermissionPending:
		return attnPermStyle.Render(glyphPermission) + " "
	case state.AttnQuestionPending:
		return attnQuestionStyle.Render(glyphQuestion) + " "
	case state.AttnErrored:
		return attnErrStyle.Render(glyphError) + " "
	}
	if source == state.SourceRecent {
		return recentStyle.Render(glyphRecent) + " "
	}
	if a == state.AttnInactive {
		return attnInactiveStyle.Render(glyphInactive) + " "
	}
	return attnActiveStyle.Render(glyphActive) + " "
}

// formatRelative renders a compact "Xm"/"Xh" duration suitable for the
// ACTIVITY column. Diffs under a minute collapse to "now"; a zero
// timestamp returns "" so the column stays empty for sessions we have
// no activity record for.
func formatRelative(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%dh", int(d/time.Hour))
}

func styledStatus(s string) string {
	switch s {
	case "busy", "generating":
		return statusBusyStyle.Render(s)
	case "", "idle":
		// idle is the default state; collapsing it removes visual noise
		// so the eye lands on rows that actually need attention.
		return ""
	default:
		return dimStyle.Render(s)
	}
}

type rowKey struct {
	instanceID string
	sessionID  string
}

func needsAttention(a state.Attention) bool {
	return a == state.AttnPermissionPending || a == state.AttnQuestionPending || a == state.AttnErrored
}

func shouldHideSubagent(sv state.SessionView) bool {
	if sv.ParentID == "" {
		return false
	}
	if needsAttention(sv.Attention) {
		return false
	}
	return sv.StatusType == "idle" || sv.StatusType == ""
}

// visibleSessions filters the snapshot rows for the sessions pane. It
// always drops finished subagents (idle leaf nodes) and reparents
// surviving children across hidden ancestors. When collapseRecent is
// true it additionally drops rows whose Source is SourceRecent so the
// pane fills with sessions the monitor is actively observing instead
// of historical context imported from /session.
//
// The second return value tracks how many recent rows belong to each
// instance (regardless of whether they were dropped) so the caller
// can surface a `▸ N recent` summary line per group.
func visibleSessions(all []state.SessionView, collapseRecent bool) ([]state.SessionView, map[string]int) {
	byKey := make(map[rowKey]state.SessionView, len(all))
	hidden := make(map[rowKey]bool, len(all))
	for _, sv := range all {
		k := rowKey{instanceID: sv.InstanceID, sessionID: sv.SessionID}
		byKey[k] = sv
		if shouldHideSubagent(sv) {
			hidden[k] = true
		}
		// When collapsing recents, treat them like hidden ancestors so
		// nearestVisibleParentID can reparent their live descendants.
		if collapseRecent && sv.Source == state.SourceRecent {
			hidden[k] = true
		}
	}

	recentByInstance := make(map[string]int)
	out := make([]state.SessionView, 0, len(all))
	for _, sv := range all {
		if shouldHideSubagent(sv) {
			continue
		}
		if sv.Source == state.SourceRecent {
			recentByInstance[sv.InstanceName]++
			if collapseRecent {
				continue
			}
		}
		sv.ParentID = nearestVisibleParentID(sv, byKey, hidden)
		out = append(out, sv)
	}
	return out, recentByInstance
}

func nearestVisibleParentID(sv state.SessionView, byKey map[rowKey]state.SessionView, hidden map[rowKey]bool) string {
	parentID := sv.ParentID
	for hops := 0; parentID != "" && hops < 32; hops++ {
		k := rowKey{instanceID: sv.InstanceID, sessionID: parentID}
		parent, ok := byKey[k]
		if !ok {
			return ""
		}
		if !hidden[k] {
			return parentID
		}
		parentID = parent.ParentID
	}
	return ""
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
	header := titleStyle.Render("opencode-monitor") + dimStyle.Render(
		fmt.Sprintf("  %d live · %d recent (≤%dm)  ·  updated %s  ·  a to %s recent  ·  q to quit",
			live, recent, int(recentWindow.Minutes()), m.snap.UpdatedAt.Format("15:04:05"),
			toggleVerb(m.recentCollapsed)))
	return header + "\n" + body + "\n" + legendLine()
}

// legendLine maps each glyph to its meaning so users can decode the
// STATE column without memorising the palette. Glyphs render in their
// attention colour; the labels are dim so the colour carries meaning.
func legendLine() string {
	parts := []string{
		dimStyle.Render("legend:"),
		attnActiveStyle.Render(glyphActive) + " " + dimStyle.Render("active"),
		attnInactiveStyle.Render(glyphInactive) + " " + dimStyle.Render("inactive"),
		recentStyle.Render(glyphRecent) + " " + dimStyle.Render("recent"),
		attnQuestionStyle.Render(glyphQuestion) + " " + dimStyle.Render("question"),
		attnPermStyle.Render(glyphPermission) + " " + dimStyle.Render("permission"),
		attnErrStyle.Render(glyphError) + " " + dimStyle.Render("error"),
	}
	return strings.Join(parts, "  ")
}

func toggleVerb(collapsed bool) string {
	if collapsed {
		return "show"
	}
	return "hide"
}

func (m model) renderAllSessions(width int, rows []state.SessionView, recentByInstance map[string]int) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Sessions") + "\n")
	if len(rows) == 0 && len(recentByInstance) == 0 {
		b.WriteString(dimStyle.Render("(no live or recent sessions on discovered instances)"))
		return b.String()
	}
	b.WriteString(columnHeader(width-2) + "\n")
	now := m.snap.UpdatedAt
	if now.IsZero() {
		now = time.Now()
	}
	// Single flat tree across all instances: live rows fill the top of
	// the pane, then a unified "▸ N recent" header acts as the divider
	// for the (optional) recent rows underneath. Per-instance grouping
	// was dropped because users want recent sessions at the bottom of
	// the list, not at the bottom of every per-instance subgroup.
	var live, recent []state.SessionView
	for _, sv := range rows {
		if sv.Source == state.SourceRecent {
			recent = append(recent, sv)
		} else {
			live = append(live, sv)
		}
	}
	if len(live) > 0 {
		b.WriteString(renderTree(now, live, width-2, sortLiveRows))
	}
	totalRecent := 0
	for _, n := range recentByInstance {
		totalRecent += n
	}
	if totalRecent > 0 {
		marker := "▸"
		if !m.recentCollapsed {
			marker = "▾"
		}
		b.WriteString(dimStyle.Render(fmt.Sprintf("%s %d recent", marker, totalRecent)) + "\n")
		if !m.recentCollapsed && len(recent) > 0 {
			b.WriteString(renderTree(now, recent, width-2, sortRecentRows))
		}
	}
	return b.String()
}

// padCell pads s with spaces so it occupies cellWidth visible cells.
// When align is lipgloss.Left, spaces trail the content; when align
// is lipgloss.Right, they lead it. Content already wider than
// cellWidth is returned unchanged (overflow tolerated, matching the
// legacy behaviour for very long titles).
func padCell(s string, cellWidth int, align lipgloss.Position) string {
	w := lipgloss.Width(s)
	if w >= cellWidth {
		return s
	}
	pad := strings.Repeat(" ", cellWidth-w)
	if align == lipgloss.Right {
		return pad + s
	}
	return s + pad
}

// columnHeader renders the dim labels above the trees so users can map
// each glyph/text fragment back to a logical column without a legend.
// The labels share their widths with formatRow's cells so headers
// always land directly above the values they describe.
func columnHeader(width int) string {
	sessionW := width - colStateW - colStatusW - colActivityW - 3*colGap
	if sessionW < 1 {
		sessionW = 1
	}
	cells := []string{
		padCell(dimStyle.Render("STATE"), colStateW, lipgloss.Left),
		padCell(dimStyle.Render("SESSION"), sessionW, lipgloss.Left),
		padCell(dimStyle.Render("STATUS"), colStatusW, lipgloss.Right),
		padCell(dimStyle.Render("ACTIVITY"), colActivityW, lipgloss.Right),
	}
	return strings.Join(cells, strings.Repeat(" ", colGap))
}

// renderTree formats a homogeneous slice (all-live or all-recent)
// into a tree of root rows with their subagents nested underneath.
// The sort policy is passed in by the caller so the live and recent
// blocks can stay on different ordering rules: live wants stable,
// attention-pinned Created ASC; recent wants LastActivity DESC since
// those rows are static by definition.
func renderTree(now time.Time, rows []state.SessionView, width int, sortRows func([]state.SessionView)) string {
	byParent := map[string][]state.SessionView{}
	knownIDs := map[string]bool{}
	for _, r := range rows {
		knownIDs[r.SessionID] = true
	}
	roots := []state.SessionView{}
	for _, r := range rows {
		if r.ParentID != "" && knownIDs[r.ParentID] {
			byParent[r.ParentID] = append(byParent[r.ParentID], r)
		} else {
			roots = append(roots, r)
		}
	}
	sortRows(roots)
	var b strings.Builder
	for _, r := range roots {
		b.WriteString(formatRow(now, r, width, false) + "\n")
		kids := byParent[r.SessionID]
		sortRows(kids)
		for _, c := range kids {
			b.WriteString(formatRow(now, c, width, true) + "\n")
		}
	}
	return b.String()
}

// sortLiveRows imposes a stable order on a homogeneous slice of live
// session rows (either all live roots or one parent's kids).
//
// Policy:
//  1. Two attention bands: rows whose Attention.Rank() < 2
//     (permission/question/errored) sort above the rest. AttnActive
//     deliberately shares the lower band with AttnInactive so a session
//     flipping busy ↔ idle between turns does not reshuffle rows.
//  2. Within each band: Created ascending — oldest session at the top,
//     new sessions drop in at the bottom of their band. Created is set
//     once at session start, so this key never changes on its own.
//  3. Fallbacks: when both Created values are zero (info fetch still
//     pending) use LastActivity descending; when only one is zero, the
//     row with a real Created value wins so resolved rows outrank
//     not-yet-resolved ones inside the same band.
//  4. Final tiebreaker: SessionID lexicographic, for determinism.
func sortLiveRows(rows []state.SessionView) {
	sort.Slice(rows, func(i, j int) bool {
		bi := rows[i].Attention.Rank() < 2
		bj := rows[j].Attention.Rank() < 2
		if bi != bj {
			return bi
		}
		ci, cj := rows[i].Created, rows[j].Created
		iZero, jZero := ci.IsZero(), cj.IsZero()
		switch {
		case !iZero && !jZero:
			// Both resolved: oldest first. If equal, fall through
			// to the SessionID tiebreaker below.
			if !ci.Equal(cj) {
				return ci.Before(cj)
			}
		case iZero != jZero:
			// One side still waiting on its /session/{id} fetch:
			// the resolved row outranks the unresolved one.
			return !iZero
		default:
			// Both unresolved: lean on LastActivity DESC so the
			// most recently touched row floats to the top of this
			// transient subset until Created lands.
			if !rows[i].LastActivity.Equal(rows[j].LastActivity) {
				return rows[i].LastActivity.After(rows[j].LastActivity)
			}
		}
		return rows[i].SessionID < rows[j].SessionID
	})
}

// sortRecentRows orders the (separately-rendered) recent block. Recent
// rows are static — they're snapshots from /session, not live SSE
// feeds — so the historical "most recently touched first" order is
// both natural and immune to the per-tick churn that motivated the
// live-block redesign. Kept distinct from sortLiveRows so the two
// blocks can evolve independently.
func sortRecentRows(rows []state.SessionView) {
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].LastActivity.Equal(rows[j].LastActivity) {
			return rows[i].LastActivity.After(rows[j].LastActivity)
		}
		return rows[i].SessionID < rows[j].SessionID
	})
}

func formatRow(now time.Time, sv state.SessionView, width int, child bool) string {
	title := sv.Title
	if title == "" {
		title = sv.Slug
	}
	if title == "" {
		title = sv.SessionID
	}
	title = trimAgentSuffix(title, sv.Agent)

	// Subagent rows get a dim "↳" prefix in the SESSION cell; the
	// STATE cell gets a matching indent below (see stateCell). The
	// two prefixes together make the hierarchy unambiguous so a
	// parent + active subagent doesn't read as two siblings.
	prefix := ""
	if child {
		prefix = dimStyle.Render("  ↳ ")
	}
	agentTag := ""
	if sv.Agent != "" {
		agentTag = agentColor(sv.Agent).Render("@" + sv.Agent)
	}
	titleRender := title
	if sv.Source == state.SourceRecent {
		titleRender = dimStyle.Render(title)
	}
	sessionContent := prefix + titleRender
	if agentTag != "" {
		sessionContent = prefix + agentTag + " " + titleRender
	}
	// CWD suffix: only on root rows. Subagents inherit their parent's
	// directory in opencode, so repeating it on every "↳" row would be
	// noise. If a subagent ever runs in a different cwd, that
	// divergence is worth surfacing — leaving room for a future
	// "show only when child differs from parent" pass without
	// changing this signature today.
	if !child && sv.Directory != "" {
		sessionContent += "  " + dimStyle.Render(shortenDirectory(sv.Directory))
	}

	sessionW := width - colStateW - colStatusW - colActivityW - 3*colGap
	if sessionW < 1 {
		sessionW = 1
	}
	// Prepend a dim "↳" to the STATE glyph for subagents so the dot
	// reads as nested under its parent rather than as a sibling
	// running session. The arrow itself supplies the visual indent;
	// no leading spaces are needed, which keeps the cell within
	// colStateW=5 ("↳ " + glyph = 3 cells).
	stateCell := attnLabel(sv.Attention, sv.Source)
	if child {
		stateCell = dimStyle.Render("↳ ") + stateCell
	}
	cells := []string{
		padCell(stateCell, colStateW, lipgloss.Left),
		padCell(sessionContent, sessionW, lipgloss.Left),
		padCell(styledStatus(sv.StatusType), colStatusW, lipgloss.Right),
		padCell(dimStyle.Render(formatRelative(now, sv.LastActivity)), colActivityW, lipgloss.Right),
	}
	return strings.Join(cells, strings.Repeat(" ", colGap))
}

func trimAgentSuffix(title, agent string) string {
	if agent == "" {
		return title
	}
	suffix := " (" + agent + ")"
	if strings.HasSuffix(title, suffix) {
		return strings.TrimSuffix(title, suffix)
	}
	return title
}

// homeDir is resolved once at startup; render-path helpers use it to
// abbreviate session directories under $HOME as "~/…". Falls back to
// empty on lookup failure, which makes shortenDirectory a no-op.
var homeDir = func() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}()

// shortenDirectory rewrites an absolute session directory into the
// compact form used in the SESSION cell. A path under $HOME becomes
// "~/<rest>"; everything else is returned unchanged. An empty input
// returns an empty string so callers can append unconditionally.
//
// Truncation past the cell width is deliberately not handled here:
// formatRow already tolerates overflow for long titles (see padCell),
// and adding a separate truncation policy for directories would
// diverge from that established behaviour.
func shortenDirectory(path string) string {
	if path == "" {
		return ""
	}
	if homeDir != "" {
		if path == homeDir {
			return "~"
		}
		if strings.HasPrefix(path, homeDir+"/") {
			return "~" + path[len(homeDir):]
		}
	}
	return path
}

func main() {
	bell := flag.Bool("bell", false, "ring terminal bell on transitions into attention states")
	status := flag.Bool("status", false, "print a one-shot icons-only attention summary and exit")
	flag.Parse()

	logF, err := os.Create("/tmp/opencode-monitor.log")
	if err == nil {
		log.SetOutput(logF)
		defer logF.Close()
	}
	if *status {
		runStatus()
		return
	}
	runTUI(*bell)
}

// runStatus is the one-shot path used by status bars and shell prompts.
// It boots discovery + supervisor, waits up to 3s for a non-empty
// snapshot, then prints a single icons-only summary line of the
// attention-bearing sessions (or an empty line if there are none) and
// exits. The TUI is never started.
func runStatus() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := state.New(ctx)
	sup := newSupervisor(store)

	events, err := discovery.Browse(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdns:", err)
		os.Exit(1)
	}
	go func() {
		for ev := range events {
			switch {
			case ev.Added != nil:
				sup.onAdd(ctx, *ev.Added)
			case ev.Removed != nil:
				sup.onRemove(ev.Removed.ID)
			}
		}
	}()

	snaps := store.Subscribe()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case snap, ok := <-snaps:
			if !ok {
				fmt.Println("")
				return
			}
			if len(snap.Sessions) == 0 {
				continue
			}
			fmt.Println(formatStatusLine(snap.Sessions))
			return
		case <-deadline.C:
			fmt.Println("")
			return
		}
	}
}

// formatStatusLine counts attention-bearing rows and renders a compact
// `<glyph> <count>` summary. Empty string means no session needs eyes.
func formatStatusLine(rows []state.SessionView) string {
	perm, question, errored := 0, 0, 0
	for _, sv := range rows {
		switch sv.Attention {
		case state.AttnPermissionPending:
			perm++
		case state.AttnQuestionPending:
			question++
		case state.AttnErrored:
			errored++
		}
	}
	var parts []string
	if question > 0 {
		parts = append(parts, fmt.Sprintf("%s %d", glyphQuestion, question))
	}
	if perm > 0 {
		parts = append(parts, fmt.Sprintf("%s %d", glyphPermission, perm))
	}
	if errored > 0 {
		parts = append(parts, fmt.Sprintf("%s %d", glyphError, errored))
	}
	return strings.Join(parts, " ")
}

func runTUI(bellEnabled bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := state.New(ctx)
	sup := newSupervisor(store)

	events, err := discovery.Browse(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdns:", err)
		os.Exit(1)
	}
	go func() {
		for ev := range events {
			switch {
			case ev.Added != nil:
				sup.onAdd(ctx, *ev.Added)
			case ev.Removed != nil:
				sup.onRemove(ev.Removed.ID)
			}
		}
	}()

	m := model{
		snaps:           store.Subscribe(),
		recentCollapsed: true,
		bellEnabled:     bellEnabled,
		bellSent:        map[rowKey]state.Attention{},
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
