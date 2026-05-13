// Package discovery finds opencode HTTP servers on the local network via
// mDNS. opencode advertises under the standard `_http._tcp.local.` service
// type with an instance name of `opencode-<port>`.
package discovery

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

// Instance is a discovered opencode HTTP server.
type Instance struct {
	ID   string // stable id: "advertised-host:port"
	Host string // host to dial; pinned to 127.0.0.1 for this localhost-only POC
	Port int
}

func (i Instance) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", i.Host, i.Port)
}

// Event is delivered on the channel returned by Browse.
type Event struct {
	Added   *Instance
	Removed *Instance
}

const serviceType = "_http._tcp"

// Browse continuously browses for opencode services and emits Event values
// on the returned channel until ctx is cancelled. Channel closes on exit.
func Browse(ctx context.Context) (<-chan Event, error) {
	// hashicorp/mdns logs noisily about IPv6 failures; silence it for the TUI.
	log.SetOutput(io.Discard)

	out := make(chan Event, 16)
	go func() {
		defer close(out)
		live := map[string]Instance{}
		for {
			if ctx.Err() != nil {
				return
			}
			pass := browseOnce(4 * time.Second)
			for id, inst := range pass {
				if _, ok := live[id]; !ok {
					i := inst
					out <- Event{Added: &i}
				}
			}
			for id, inst := range live {
				if _, ok := pass[id]; !ok {
					i := inst
					out <- Event{Removed: &i}
				}
			}
			live = pass
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
	return out, nil
}

func browseOnce(d time.Duration) map[string]Instance {
	entries := make(chan *mdns.ServiceEntry, 32)
	out := map[string]Instance{}
	done := make(chan struct{})
	go func() {
		for e := range entries {
			inst := strings.SplitN(e.Name, ".", 2)[0]
			if !strings.HasPrefix(inst, "opencode-") {
				continue
			}
			advertised := ""
			if e.AddrV4 != nil {
				advertised = e.AddrV4.String()
			} else if e.Host != "" {
				advertised = strings.TrimSuffix(e.Host, ".")
			}
			if advertised == "" {
				continue
			}
			id := fmt.Sprintf("%s:%d", advertised, e.Port)
			// Localhost-only POC: opencode rejects requests to non-loopback
			// hostnames even when bound on 0.0.0.0, so always dial 127.0.0.1.
			out[id] = Instance{ID: id, Host: "127.0.0.1", Port: e.Port}
		}
		close(done)
	}()
	_ = mdns.Query(&mdns.QueryParam{
		Service:     serviceType,
		Domain:      "local",
		Timeout:     d,
		Entries:     entries,
		DisableIPv6: true,
	})
	close(entries)
	<-done
	return out
}
