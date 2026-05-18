package oc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guilhermehto/cogitator/internal/config"
)

func TestGetSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/s1" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Session{ID: "s1", Title: "alpha", Slug: "alpha", Directory: "/tmp", Time: SessionTime{Created: 1, Updated: 2}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, config.Default())
	s, err := c.GetSession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GetSession error: %v", err)
	}
	if s.ID != "s1" || s.Title != "alpha" {
		t.Fatalf("unexpected session: %+v", s)
	}
}

func TestListRecentSessionsFiltersByWindow(t *testing.T) {
	now := time.Now()
	inside := now.Add(-5 * time.Minute).UnixMilli()
	outside := now.Add(-2 * time.Hour).UnixMilli()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]Session{
			{ID: "inside", Title: "inside", Slug: "inside", Directory: "/tmp", Time: SessionTime{Created: 1, Updated: inside}},
			{ID: "outside", Title: "outside", Slug: "outside", Directory: "/tmp", Time: SessionTime{Created: 1, Updated: outside}},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, config.Default())
	rows, err := c.ListRecentSessions(context.Background(), 30*time.Minute)
	if err != nil {
		t.Fatalf("ListRecentSessions error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "inside" {
		t.Fatalf("unexpected filtered rows: %+v", rows)
	}
}

func TestPendingPermissions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/permission" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]PermissionRequest{{ID: "p1", SessionID: "s1", Permission: "shell"}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, config.Default())
	perms, err := c.PendingPermissions(context.Background())
	if err != nil {
		t.Fatalf("PendingPermissions error: %v", err)
	}
	if len(perms) != 1 || perms[0].ID != "p1" || perms[0].SessionID != "s1" {
		t.Fatalf("unexpected permissions: %+v", perms)
	}
}
