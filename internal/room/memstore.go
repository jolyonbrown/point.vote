package room

import (
	"sync"
	"time"
)

// MemStore is the v1 Store: a map behind an RWMutex (the "Hub" of PLAN.md
// §8). Rooms live in RAM and evaporate; that is a feature.
type MemStore struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{rooms: make(map[string]*Room)}
}

func (s *MemStore) Put(r *Room) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rooms[r.ID()]; exists {
		return ErrIDTaken
	}
	if len(s.rooms) >= MaxRooms {
		return ErrServerFull
	}
	s.rooms[r.ID()] = r
	return nil
}

func (s *MemStore) Get(id string) (*Room, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rooms[id]
	return r, ok
}

func (s *MemStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.rooms)
}

func (s *MemStore) Expire(cutoff time.Time) []*Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []*Room
	for id, r := range s.rooms {
		if r.LastActiveAt().Before(cutoff) {
			expired = append(expired, r)
			delete(s.rooms, id)
		}
	}
	return expired
}
