package ui

import (
	"sort"

	"github.com/guilhermehto/cogitator/internal/state"
)

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
			if !ci.Equal(cj) {
				return ci.Before(cj)
			}
		case iZero != jZero:
			return !iZero
		default:
			if !rows[i].LastActivity.Equal(rows[j].LastActivity) {
				return rows[i].LastActivity.After(rows[j].LastActivity)
			}
		}

		return rows[i].SessionID < rows[j].SessionID
	})
}

func sortRecentRows(rows []state.SessionView) {
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].LastActivity.Equal(rows[j].LastActivity) {
			return rows[i].LastActivity.After(rows[j].LastActivity)
		}
		return rows[i].SessionID < rows[j].SessionID
	})
}
