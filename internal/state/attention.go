package state

import "time"

// Attention is a coarse triage label derived from session status, pending
// permissions, and event history.
type Attention string

const (
	AttnActive            Attention = "active"
	AttnPermissionPending Attention = "permission"
	AttnErrored           Attention = "errored"
	AttnIdleWaiting       Attention = "idle-waiting"
)

// Rank is used to sort the attention pane: lower = more urgent.
func (a Attention) Rank() int {
	switch a {
	case AttnPermissionPending:
		return 0
	case AttnErrored:
		return 1
	case AttnIdleWaiting:
		return 2
	default:
		return 3
	}
}

// IdleWaitingThreshold is how long since the last activity counts as
// "idle-waiting" for an otherwise-idle session.
const IdleWaitingThreshold = 30 * time.Second

// Classify computes the attention label for one session.
//
// statusType is the value of SessionStatus.type ("idle", "generating",
// "retry", ...). hasPermission means a pending permission request exists for
// this session. lastError is the time of the most recent session.error event
// (zero if none). lastActivity is the time of the most recent
// message/session update. now is wall-clock time injected for testability.
func Classify(statusType string, hasPermission bool, lastError, lastActivity, now time.Time) Attention {
	if hasPermission {
		return AttnPermissionPending
	}
	// An error counts as needing attention until something newer happens.
	if !lastError.IsZero() && !lastError.Before(lastActivity) {
		return AttnErrored
	}
	if statusType == "idle" || statusType == "" {
		if lastActivity.IsZero() || now.Sub(lastActivity) >= IdleWaitingThreshold {
			return AttnIdleWaiting
		}
	}
	return AttnActive
}
