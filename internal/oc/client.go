package oc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/guilhermehto/cogitator/internal/config"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL string, cfg *config.Config) *Client {
	if cfg == nil {
		cfg = config.Default()
	}
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: cfg.HTTPTimeout},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// GetSession looks up a single session by ID. Used to resolve titles for
// session IDs we first learn about via SSE events.
func (c *Client) GetSession(ctx context.Context, id string) (Session, error) {
	var s Session
	return s, c.get(ctx, "/session/"+id, &s)
}

// ListRecentSessions returns sessions from /session (project-scoped to the
// instance's working directory) whose time.updated falls within the recency
// window. opencode has no "currently open in some TUI" query, so recency is
// the best proxy for discovering sessions the user is likely working on.
//
// We deliberately use /session (project-scoped) instead of
// /experimental/session (global) so each opencode instance only contributes
// its own project's sessions — multiple instances against the same database
// would otherwise return identical lists and the view would duplicate.
func (c *Client) ListRecentSessions(ctx context.Context, window time.Duration) ([]Session, error) {
	var all []Session
	if err := c.get(ctx, "/session", &all); err != nil {
		return nil, err
	}
	cutoffMs := time.Now().Add(-window).UnixMilli()
	out := all[:0]
	for _, s := range all {
		if s.Time.Updated >= cutoffMs {
			out = append(out, s)
		}
	}
	return out, nil
}

// PendingPermissions returns currently outstanding permission requests.
func (c *Client) PendingPermissions(ctx context.Context) ([]PermissionRequest, error) {
	var ps []PermissionRequest
	return ps, c.get(ctx, "/permission", &ps)
}
