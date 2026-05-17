package oc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// globalEventEnvelope wraps the payload that GET /global/event sends.
// /global/event delivers events for sessions in every directory; /event is
// scoped to the requesting client's directory and is silent for an external
// observer like cogitator. Each record looks like:
//
//	{"directory":"...","project":"...","payload":{"id":"evt_…","type":"…","properties":{…}}}
//
// We unwrap and forward the payload.
type globalEventEnvelope struct {
	Directory string          `json:"directory"`
	Project   string          `json:"project"`
	Payload   json.RawMessage `json:"payload"`
}

// SubscribeEvents connects to GET /global/event and streams decoded Event
// values into out until ctx is cancelled or the connection drops. It does not
// reconnect; the caller's lifecycle goroutine is responsible for that.
func (c *Client) SubscribeEvents(ctx context.Context, out chan<- Event) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/global/event", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	httpc := &http.Client{Timeout: 0, Transport: c.HTTP.Transport}
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /global/event: %s", resp.Status)
	}

	br := bufio.NewReaderSize(resp.Body, 64<<10)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		var env globalEventEnvelope
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			continue
		}
		// Try wrapped form first; if Payload is empty, treat the whole record
		// as a flat Event (server.connected uses the flat form).
		var evt Event
		target := env.Payload
		if len(target) == 0 {
			target = []byte(raw)
		}
		if err := json.Unmarshal(target, &evt); err != nil {
			continue
		}
		// Skip wrapper-only records (no usable type).
		if evt.Type == "" {
			continue
		}
		// Drop the noisy "sync" mirror events; they duplicate the real ones.
		if evt.Type == "sync" {
			continue
		}
		select {
		case out <- evt:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// SleepBackoff returns a simple linear backoff for reconnect loops.
func SleepBackoff(ctx context.Context, attempt int) {
	d := time.Duration(attempt) * time.Second
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	if d <= 0 {
		d = time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
