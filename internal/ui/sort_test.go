package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/guilhermehto/cogitator/internal/state"
)

func idsAfterSort(rows []state.SessionView) []string {
	sortLiveRows(rows)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.SessionID
	}
	return out
}

func sortRow(id string, attn state.Attention, created, lastActivity time.Time) state.SessionView {
	sv := liveSessionView(id, "", "idle", attn)
	sv.Created = created
	sv.LastActivity = lastActivity
	return sv
}

func TestSortLiveRowsByCreatedAsc(t *testing.T) {
	t0 := time.Unix(1_000, 0)
	t1 := time.Unix(2_000, 0)
	t2 := time.Unix(3_000, 0)
	rows := []state.SessionView{
		sortRow("c", state.AttnInactive, t2, time.Time{}),
		sortRow("a", state.AttnInactive, t0, time.Time{}),
		sortRow("b", state.AttnInactive, t1, time.Time{}),
	}
	got := idsAfterSort(rows)
	want := []string{"a", "b", "c"}
	if !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSortLiveRowsAttentionPinsAboveBand(t *testing.T) {
	old := time.Unix(1_000, 0)
	newer := time.Unix(5_000, 0)
	rows := []state.SessionView{
		sortRow("inactive-old", state.AttnInactive, old, time.Time{}),
		sortRow("perm-new", state.AttnPermissionPending, newer, time.Time{}),
	}
	got := idsAfterSort(rows)
	want := []string{"perm-new", "inactive-old"}
	if !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSortLiveRowsAllUpperBandAttentionsPinned(t *testing.T) {
	old := time.Unix(1_000, 0)
	newer := time.Unix(5_000, 0)
	cases := []struct {
		name string
		attn state.Attention
	}{
		{"permission", state.AttnPermissionPending},
		{"question", state.AttnQuestionPending},
		{"errored", state.AttnErrored},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := []state.SessionView{
				sortRow("inactive-old", state.AttnInactive, old, time.Time{}),
				sortRow("upper", c.attn, newer, time.Time{}),
			}
			got := idsAfterSort(rows)
			want := []string{"upper", "inactive-old"}
			if !equalStrings(got, want) {
				t.Fatalf("attn=%v order = %v, want %v", c.attn, got, want)
			}
		})
	}
}

func TestSortLiveRowsActiveNotPinned(t *testing.T) {
	old := time.Unix(1_000, 0)
	newer := time.Unix(5_000, 0)
	rows := []state.SessionView{
		sortRow("inactive-old", state.AttnInactive, old, time.Time{}),
		sortRow("active-new", state.AttnActive, newer, time.Time{}),
	}
	got := idsAfterSort(rows)
	want := []string{"inactive-old", "active-new"}
	if !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSortLiveRowsAttentionBandSortedByCreated(t *testing.T) {
	t0 := time.Unix(1_000, 0)
	t1 := time.Unix(2_000, 0)
	rows := []state.SessionView{
		sortRow("perm-new", state.AttnPermissionPending, t1, time.Time{}),
		sortRow("perm-old", state.AttnPermissionPending, t0, time.Time{}),
	}
	got := idsAfterSort(rows)
	want := []string{"perm-old", "perm-new"}
	if !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSortLiveRowsZeroCreatedFallback(t *testing.T) {
	a := time.Unix(1_000, 0)
	b := time.Unix(2_000, 0)
	rows := []state.SessionView{
		sortRow("older", state.AttnInactive, time.Time{}, a),
		sortRow("newer", state.AttnInactive, time.Time{}, b),
	}
	got := idsAfterSort(rows)
	want := []string{"newer", "older"}
	if !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSortLiveRowsZeroVsNonZero(t *testing.T) {
	created := time.Unix(5_000, 0)
	activity := time.Unix(9_000, 0)
	rows := []state.SessionView{
		sortRow("unresolved", state.AttnInactive, time.Time{}, activity),
		sortRow("resolved", state.AttnInactive, created, time.Time{}),
	}
	got := idsAfterSort(rows)
	want := []string{"resolved", "unresolved"}
	if !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSortRecentRowsByLastActivityDesc(t *testing.T) {
	older := time.Unix(1_000, 0)
	newer := time.Unix(5_000, 0)
	createdOld := time.Unix(100, 0)
	createdNew := time.Unix(200, 0)

	rows := []state.SessionView{
		{SessionID: "old-touch", Source: state.SourceRecent, Created: createdOld, LastActivity: older},
		{SessionID: "new-touch", Source: state.SourceRecent, Created: createdNew, LastActivity: newer},
	}
	sortRecentRows(rows)
	if rows[0].SessionID != "new-touch" || rows[1].SessionID != "old-touch" {
		t.Fatalf("order = [%s, %s], want [new-touch, old-touch]", rows[0].SessionID, rows[1].SessionID)
	}
}

func TestRenderTreeAppliesSortToKids(t *testing.T) {
	t0 := time.Unix(1_000, 0)
	t1 := time.Unix(2_000, 0)
	t2 := time.Unix(3_000, 0)

	root := sortRow("root", state.AttnInactive, t0, time.Time{})

	kidOld := sortRow("kid-old", state.AttnInactive, t1, time.Time{})
	kidOld.ParentID = "root"
	kidNewPerm := sortRow("kid-perm", state.AttnPermissionPending, t2, time.Time{})
	kidNewPerm.ParentID = "root"

	out := renderTree(time.Unix(10_000, 0), []state.SessionView{root, kidOld, kidNewPerm}, 200, sortLiveRows)

	permIdx := strings.Index(out, "kid-perm")
	oldIdx := strings.Index(out, "kid-old")
	if permIdx < 0 || oldIdx < 0 {
		t.Fatalf("missing kids in output:\n%s", out)
	}
	if permIdx > oldIdx {
		t.Fatalf("kid attention pin not applied: perm at %d, old at %d\n%s", permIdx, oldIdx, out)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
