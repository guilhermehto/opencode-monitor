package supervisor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/guilhermehto/cogitator/internal/config"
	"github.com/guilhermehto/cogitator/internal/discovery"
	"github.com/guilhermehto/cogitator/internal/oc"
	"github.com/guilhermehto/cogitator/internal/state"
)

type instanceLifecycle struct {
	cancel context.CancelFunc
}

type Supervisor struct {
	mu        sync.Mutex
	store     *state.Store
	instances map[string]*instanceLifecycle
	cfg       *config.Config
	logger    *slog.Logger
}

func New(store *state.Store, cfg *config.Config, logger *slog.Logger) *Supervisor {
	if cfg == nil {
		cfg = config.Default()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{
		store:     store,
		instances: map[string]*instanceLifecycle{},
		cfg:       cfg,
		logger:    logger,
	}
}

func (s *Supervisor) OnAdd(parent context.Context, inst discovery.Instance) {
	s.mu.Lock()
	if _, exists := s.instances[inst.ID]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.instances[inst.ID] = &instanceLifecycle{cancel: cancel}
	s.mu.Unlock()

	s.store.AddInstance(inst)
	go s.run(ctx, inst)
}

func (s *Supervisor) OnRemove(id string) {
	s.mu.Lock()
	lc := s.instances[id]
	delete(s.instances, id)
	s.mu.Unlock()
	if lc != nil {
		lc.cancel()
	}
	s.store.RemoveInstance(id)
}

func (s *Supervisor) run(ctx context.Context, inst discovery.Instance) {
	client := oc.NewClient(inst.BaseURL(), s.cfg)

	syncPerms := func() {
		pctx, cancel := context.WithTimeout(ctx, s.cfg.PermissionSyncTimeout)
		defer cancel()
		perms, err := client.PendingPermissions(pctx)
		if err != nil {
			s.logger.Warn("permission sync failed", "instance", inst.ID, "err", err)
			s.store.RecordInstanceError(inst.ID, err)
			return
		}
		s.store.RecordInstanceSuccess(inst.ID)
		s.store.SyncPermissions(inst.ID, perms)
	}

	syncRecent := func() {
		rctx, cancel := context.WithTimeout(ctx, s.cfg.RecentSyncTimeout)
		defer cancel()
		sessions, err := client.ListRecentSessions(rctx, s.cfg.RecentWindow)
		if err != nil {
			s.logger.Warn("recent session sync failed", "instance", inst.ID, "err", err)
			s.store.RecordInstanceError(inst.ID, err)
			return
		}
		s.store.RecordInstanceSuccess(inst.ID)
		s.store.SyncRecent(inst.ID, sessions)
	}

	syncPerms()
	syncRecent()

	pTicker := time.NewTicker(s.cfg.PollEvery)
	defer pTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pTicker.C:
				syncPerms()
			}
		}
	}()

	rTicker := time.NewTicker(s.cfg.RecencyPollEvery)
	defer rTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-rTicker.C:
				syncRecent()
			}
		}
	}()

	attempt := 0
	for ctx.Err() == nil {
		events := make(chan oc.Event, 32)
		done := make(chan error, 1)
		go func() { done <- client.SubscribeEvents(ctx, events) }()

	stream:
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-events:
				attempt = 0
				s.store.RecordInstanceSuccess(inst.ID)
				s.store.ApplyEvent(inst.ID, evt)
			case err := <-done:
				if err != nil && ctx.Err() == nil {
					s.logger.Warn("event stream dropped", "instance", inst.ID, "err", err)
					s.store.RecordInstanceError(inst.ID, err)
				}
				break stream
			}
		}

		attempt++
		oc.SleepBackoff(ctx, s.cfg, attempt)
	}
}
