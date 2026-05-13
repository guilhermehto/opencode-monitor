package oc

import "encoding/json"

// Session is the minimum subset of opencode's Session schema we render.
type Session struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	ParentID  string `json:"parentID,omitempty"` // set on subagent sessions
	Agent     string `json:"agent,omitempty"`    // set on subagent sessions
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

// Status mirrors a SessionStatus union; we only need the discriminator and
// any free-form message for display. Real values seen in events: "idle",
// "busy", "retry".
type Status struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// PermissionRequest matches /permission entries and permission.asked payload.
type PermissionRequest struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionID"`
	Permission string `json:"permission"`
}

// Event is the SSE envelope. Properties stays as raw JSON and is decoded by
// the dispatcher based on Type.
type Event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

// Typed event payloads.

type SessionInfoEvt struct {
	SessionID string  `json:"sessionID"`
	Info      Session `json:"info"`
}

type SessionIDEvt struct {
	SessionID string `json:"sessionID"`
}

type SessionStatusEvt struct {
	SessionID string `json:"sessionID"`
	Status    Status `json:"status"`
}

type SessionErrorEvt struct {
	SessionID string          `json:"sessionID"`
	Error     json.RawMessage `json:"error"`
}

type PermissionRepliedEvt struct {
	SessionID string `json:"sessionID"`
	RequestID string `json:"requestID"`
	Reply     string `json:"reply"`
}
