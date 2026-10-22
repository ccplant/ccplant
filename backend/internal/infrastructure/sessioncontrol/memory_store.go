package sessioncontrol

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
)

// MemoryStore provides the per-process session-control streams needed by the
// native manager, which intentionally has no Redis dependency.
type MemoryStore struct {
	mu          sync.Mutex
	nextID      uint64
	commands    map[string][]core.Command
	events      map[string][]core.Event
	commandAcks map[string]uint64
	eventIDs    map[string]map[string]bool
	connectedAt map[string]time.Time
	wake        map[string]chan struct{}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		commands: make(map[string][]core.Command), events: make(map[string][]core.Event),
		commandAcks: make(map[string]uint64), eventIDs: make(map[string]map[string]bool),
		connectedAt: make(map[string]time.Time), wake: make(map[string]chan struct{}),
	}
}

func (s *MemoryStore) TouchConnection(_ context.Context, sessionID string) error {
	s.mu.Lock()
	s.connectedAt[sessionID] = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) IsConnected(_ context.Context, sessionID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.connectedAt[sessionID]) <= connectionTTL, nil
}

func (s *MemoryStore) EnqueueCommand(_ context.Context, sessionID string, command core.Command) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.newIDLocked()
	command.StreamID = id
	s.commands[sessionID] = append(s.commands[sessionID], command)
	s.notifyLocked(sessionID)
	return id, nil
}

func (s *MemoryStore) ReadCommands(ctx context.Context, sessionID, after string, wait time.Duration, count int64) ([]core.Command, error) {
	for {
		s.mu.Lock()
		cursor := max(parseMemoryStreamID(after), s.commandAcks[sessionID])
		result := commandsAfter(s.commands[sessionID], cursor, count)
		wake := s.wakeLocked(sessionID)
		s.mu.Unlock()
		if len(result) > 0 || wait <= 0 {
			return result, nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
			return nil, nil
		case <-wake:
			timer.Stop()
			wait = 0
		}
	}
}

func (s *MemoryStore) AckCommand(_ context.Context, sessionID, streamID string) error {
	s.mu.Lock()
	s.commandAcks[sessionID] = max(s.commandAcks[sessionID], parseMemoryStreamID(streamID))
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) AppendEvents(_ context.Context, sessionID string, events []core.Event) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eventIDs[sessionID] == nil {
		s.eventIDs[sessionID] = make(map[string]bool)
	}
	last := ""
	for _, event := range events {
		if s.eventIDs[sessionID][event.ID] {
			continue
		}
		s.eventIDs[sessionID][event.ID] = true
		last = s.newIDLocked()
		event.StreamID = last
		s.events[sessionID] = append(s.events[sessionID], event)
	}
	return last, nil
}

func (s *MemoryStore) ReadEvents(_ context.Context, sessionID, after string, _ time.Duration, count int64) ([]core.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor := parseMemoryStreamID(after)
	limit := normalizeMemoryCount(count)
	result := make([]core.Event, 0, limit)
	for _, event := range s.events[sessionID] {
		if parseMemoryStreamID(event.StreamID) > cursor {
			result = append(result, event)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (s *MemoryStore) newIDLocked() string {
	s.nextID++
	return fmt.Sprintf("%d-0", s.nextID)
}

func (s *MemoryStore) wakeLocked(sessionID string) chan struct{} {
	if s.wake[sessionID] == nil {
		s.wake[sessionID] = make(chan struct{})
	}
	return s.wake[sessionID]
}

func (s *MemoryStore) notifyLocked(sessionID string) {
	if wake := s.wake[sessionID]; wake != nil {
		close(wake)
		s.wake[sessionID] = make(chan struct{})
	}
}

func commandsAfter(commands []core.Command, cursor uint64, count int64) []core.Command {
	limit := normalizeMemoryCount(count)
	result := make([]core.Command, 0, limit)
	for _, command := range commands {
		if parseMemoryStreamID(command.StreamID) > cursor {
			result = append(result, command)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func normalizeMemoryCount(count int64) int {
	if count <= 0 || count > 100 {
		return 100
	}
	return int(count)
}

func parseMemoryStreamID(value string) uint64 {
	head, _, _ := strings.Cut(value, "-")
	id, _ := strconv.ParseUint(head, 10, 64)
	return id
}
