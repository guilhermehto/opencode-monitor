package oc

import "encoding/json"

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
