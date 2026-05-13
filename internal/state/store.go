// Package state holds the in-memory model the TUI renders. Sessions enter
// the view from two sources:
//
//   - "live": observed via an SSE event (or pending-permission poll) during
//     this monitor run. These get the full attention classification.
//   - "recent": imported from /experimental/session because they were touched
//     within the recency window. Treated as discovery context only — they
//     never trigger IDLE-WAIT attention on their own. Promoted to "live"
//     the moment any event arrives for them.
package state

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/goliveira/opencode-monitor/internal/discovery"
	"github.com/goliveira/opencode-monitor/internal/oc"
)

type Source string

const (
	SourceLive   Source = "live"
	SourceRecent Source = "recent"
)

type SessionView struct {
	InstanceID   string
	InstanceName string
	SessionID    string
	Title        string
	Slug         string
	Directory    string
	ParentID     string
	Agent        string
	StatusType   string
	Source       Source
	Attention    Attention
	LastActivity time.Time
}

type Snapshot struct {
	Sessions  []SessionView
	UpdatedAt time.Time
}

type sessionRow struct {
	info         oc.Session
	status       oc.Status
	hasPerm      bool
	source       Source
	lastError    time.Time
	lastActivity time.Time
}

type instanceState struct {
	id       string
	name     string
	client   *oc.Client
	sessions map[string]*sessionRow
	perms    map[string]string
}

type Store struct {
	mu        sync.Mutex
	instances map[string]*instanceState
	listeners []chan Snapshot
	now       func() time.Time
	lookupCtx context.Context
}

func New(ctx context.Context) *Store {
	return &Store{
		instances: map[string]*instanceState{},
		now:       time.Now,
		lookupCtx: ctx,
	}
}

func (s *Store) Subscribe() <-chan Snapshot {
	ch := make(chan Snapshot, 4)
	s.mu.Lock()
	s.listeners = append(s.listeners, ch)
	s.mu.Unlock()
	s.publish()
	return ch
}

func (s *Store) AddInstance(inst discovery.Instance) {
	s.mu.Lock()
	if _, ok := s.instances[inst.ID]; !ok {
		s.instances[inst.ID] = &instanceState{
			id:       inst.ID,
			name:     inst.ID,
			client:   oc.NewClient(inst.BaseURL()),
			sessions: map[string]*sessionRow{},
			perms:    map[string]string{},
		}
	}
	s.mu.Unlock()
	s.publish()
}

func (s *Store) RemoveInstance(id string) {
	s.mu.Lock()
	delete(s.instances, id)
	s.mu.Unlock()
	s.publish()
}

// SyncRecent imports sessions from a recency-window /experimental/session
// fetch. New rows land as "recent"; rows already present (live OR recent)
// just get fresher metadata merged in.
func (s *Store) SyncRecent(instanceID string, sessions []oc.Session) {
	s.mu.Lock()
	inst := s.instances[instanceID]
	if inst == nil {
		s.mu.Unlock()
		return
	}
	for _, info := range sessions {
		row, exists := inst.sessions[info.ID]
		if !exists {
			row = &sessionRow{source: SourceRecent}
			inst.sessions[info.ID] = row
		}
		mergeSessionInfo(&row.info, info)
		// Seed a baseline lastActivity from the server's update timestamp so
		// the pane sorts sensibly even before any event arrives.
		if info.Time.Updated > 0 {
			ts := time.UnixMilli(info.Time.Updated)
			if ts.After(row.lastActivity) {
				row.lastActivity = ts
			}
		}
	}
	s.mu.Unlock()
	s.publish()
}

func (s *Store) SyncPermissions(instanceID string, perms []oc.PermissionRequest) {
	s.mu.Lock()
	inst := s.instances[instanceID]
	if inst == nil {
		s.mu.Unlock()
		return
	}
	inst.perms = map[string]string{}
	wantSessions := map[string]bool{}
	for _, p := range perms {
		inst.perms[p.ID] = p.SessionID
		wantSessions[p.SessionID] = true
	}
	for sid := range wantSessions {
		// A pending permission promotes a session to "live" — it needs
		// attention right now regardless of how we first heard of it.
		row := s.touchLocked(inst, sid)
		row.source = SourceLive
	}
	for _, row := range inst.sessions {
		row.hasPerm = sessionHasPermission(inst, row.info.ID)
	}
	s.mu.Unlock()
	s.publish()
}

func (s *Store) Republish() { s.publish() }

func (s *Store) ApplyEvent(instanceID string, evt oc.Event) {
	s.mu.Lock()
	inst := s.instances[instanceID]
	if inst == nil {
		s.mu.Unlock()
		return
	}
	now := s.now()

	// Helper used by every case: any event for a session promotes it to live.
	promote := func(sid string) *sessionRow {
		row := s.touchLocked(inst, sid)
		row.source = SourceLive
		return row
	}

	switch evt.Type {
	case "session.created", "session.updated":
		var p oc.SessionInfoEvt
		if json.Unmarshal(evt.Properties, &p) == nil {
			row := promote(p.SessionID)
			mergeSessionInfo(&row.info, p.Info)
			row.lastActivity = now
		}
	case "session.deleted":
		var p oc.SessionInfoEvt
		if json.Unmarshal(evt.Properties, &p) == nil {
			delete(inst.sessions, p.SessionID)
		}
	case "session.status":
		var p oc.SessionStatusEvt
		if json.Unmarshal(evt.Properties, &p) == nil {
			row := promote(p.SessionID)
			row.status = p.Status
			row.lastActivity = now
		}
	case "session.idle":
		var p oc.SessionIDEvt
		if json.Unmarshal(evt.Properties, &p) == nil {
			row := promote(p.SessionID)
			if row.status.Type == "" {
				row.status = oc.Status{Type: "idle"}
			}
			row.lastActivity = now
		}
	case "session.error":
		var p oc.SessionErrorEvt
		if json.Unmarshal(evt.Properties, &p) == nil {
			row := promote(p.SessionID)
			row.lastError = now
		}
	case "permission.asked":
		var p oc.PermissionRequest
		if json.Unmarshal(evt.Properties, &p) == nil {
			inst.perms[p.ID] = p.SessionID
			row := promote(p.SessionID)
			row.hasPerm = true
		}
	case "permission.replied":
		var p oc.PermissionRepliedEvt
		if json.Unmarshal(evt.Properties, &p) == nil {
			delete(inst.perms, p.RequestID)
			if row, ok := inst.sessions[p.SessionID]; ok {
				row.hasPerm = sessionHasPermission(inst, p.SessionID)
			}
		}
	case "message.updated", "message.part.updated", "message.part.delta":
		var p struct {
			SessionID string `json:"sessionID"`
			Info      *struct {
				SessionID string `json:"sessionID"`
			} `json:"info"`
			Part *struct {
				SessionID string `json:"sessionID"`
			} `json:"part"`
		}
		if json.Unmarshal(evt.Properties, &p) == nil {
			sid := p.SessionID
			if sid == "" && p.Info != nil {
				sid = p.Info.SessionID
			}
			if sid == "" && p.Part != nil {
				sid = p.Part.SessionID
			}
			if sid != "" {
				row := promote(sid)
				row.lastActivity = now
			}
		}
	}
	s.mu.Unlock()
	s.publish()
}

func mergeSessionInfo(dst *oc.Session, src oc.Session) {
	if src.ID != "" {
		dst.ID = src.ID
	}
	if src.Slug != "" {
		dst.Slug = src.Slug
	}
	if src.Title != "" {
		dst.Title = src.Title
	}
	if src.Directory != "" {
		dst.Directory = src.Directory
	}
	if src.ParentID != "" {
		dst.ParentID = src.ParentID
	}
	if src.Agent != "" {
		dst.Agent = src.Agent
	}
	if src.Time.Created > 0 {
		dst.Time.Created = src.Time.Created
	}
	if src.Time.Updated > 0 {
		dst.Time.Updated = src.Time.Updated
	}
}

func (s *Store) touchLocked(inst *instanceState, sid string) *sessionRow {
	if sid == "" {
		return &sessionRow{}
	}
	row, ok := inst.sessions[sid]
	if ok {
		return row
	}
	row = &sessionRow{}
	row.info.ID = sid
	inst.sessions[sid] = row
	go s.fetchSessionInfo(inst.id, sid)
	return row
}

func (s *Store) fetchSessionInfo(instanceID, sid string) {
	s.mu.Lock()
	inst := s.instances[instanceID]
	var client *oc.Client
	if inst != nil {
		client = inst.client
	}
	s.mu.Unlock()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.lookupCtx, 5*time.Second)
	defer cancel()
	info, err := client.GetSession(ctx, sid)
	if err != nil {
		return
	}
	s.mu.Lock()
	inst = s.instances[instanceID]
	if inst != nil {
		if row, ok := inst.sessions[sid]; ok {
			mergeSessionInfo(&row.info, info)
		}
	}
	s.mu.Unlock()
	s.publish()
}

func sessionHasPermission(inst *instanceState, sid string) bool {
	for _, s := range inst.perms {
		if s == sid {
			return true
		}
	}
	return false
}

func (s *Store) publish() {
	snap := s.snapshot()
	s.mu.Lock()
	listeners := append([]chan Snapshot(nil), s.listeners...)
	s.mu.Unlock()
	for _, ch := range listeners {
		select {
		case ch <- snap:
		default:
		}
	}
}

func (s *Store) snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	rows := make([]SessionView, 0, 32)
	for _, inst := range s.instances {
		for _, row := range inst.sessions {
			rows = append(rows, SessionView{
				InstanceID:   inst.id,
				InstanceName: inst.name,
				SessionID:    row.info.ID,
				Title:        row.info.Title,
				Slug:         row.info.Slug,
				Directory:    row.info.Directory,
				ParentID:     row.info.ParentID,
				Agent:        row.info.Agent,
				StatusType:   row.status.Type,
				Source:       row.source,
				Attention:    classifyForSource(row, now),
				LastActivity: row.lastActivity,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].InstanceID != rows[j].InstanceID {
			return rows[i].InstanceID < rows[j].InstanceID
		}
		return rows[i].LastActivity.After(rows[j].LastActivity)
	})
	return Snapshot{Sessions: rows, UpdatedAt: now}
}

func classifyForSource(row *sessionRow, now time.Time) Attention {
	a := Classify(row.status.Type, row.hasPerm, row.lastError, row.lastActivity, now)
	// "recent" sessions weren't observed transitioning to idle by us; suppress
	// IDLE-WAIT so the attention pane stays meaningful. Permission/error
	// signals are real-time and still apply.
	if row.source == SourceRecent && a == AttnIdleWaiting {
		return AttnActive
	}
	return a
}
