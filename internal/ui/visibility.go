package ui

import "github.com/guilhermehto/cogitator/internal/state"

func shouldHideSubagent(sv state.SessionView) bool {
	if sv.ParentID == "" {
		return false
	}
	if needsAttention(sv.Attention) {
		return false
	}
	return sv.StatusType == "idle" || sv.StatusType == ""
}

// visibleSessions filters snapshot rows for the sessions pane.
func visibleSessions(all []state.SessionView, collapseRecent bool) ([]state.SessionView, map[string]int) {
	byKey := make(map[rowKey]state.SessionView, len(all))
	hidden := make(map[rowKey]bool, len(all))
	for _, sv := range all {
		k := rowKey{instanceID: sv.InstanceID, sessionID: sv.SessionID}
		byKey[k] = sv
		if shouldHideSubagent(sv) {
			hidden[k] = true
		}
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
