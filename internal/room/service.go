package room

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Service is the single façade over the domain: the REST API and, later,
// the MCP tools both call it, so there is zero duplicated logic between
// surfaces. It also owns the domain event log (PLAN.md §7) — note that
// vote_cast never logs the value.
type Service struct {
	store Store
	log   *slog.Logger

	// injectable for tests
	now   func() time.Time
	genID func() string
}

// NewService wires a Service over a store.
func NewService(store Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log, now: time.Now, genID: RandomID}
}

// CreateRoom validates inputs, mints a memorable ID (regenerating on
// collision) and registers the room. Creating does not auto-join.
func (s *Service) CreateRoom(deck []string, subject, context string, autoReveal bool) (State, error) {
	if err := ValidateDeck(deck); err != nil {
		return State{}, err
	}
	if err := validateRoundFields(subject, context); err != nil {
		return State{}, err
	}
	for range 20 {
		r := NewRoom(s.genID(), deck, subject, context, autoReveal, s.now())
		err := s.store.Put(r)
		if errors.Is(err, ErrIDTaken) {
			continue
		}
		if err != nil {
			return State{}, err
		}
		s.log.Info("room_created", "room_id", r.ID(), "deck_size", len(deck), "auto_reveal", autoReveal)
		return r.Snapshot(s.now()), nil
	}
	// 20 straight collisions means the ID space is effectively exhausted.
	return State{}, ErrServerFull
}

func (s *Service) room(id string) (*Room, error) {
	r, ok := s.store.Get(id)
	if !ok {
		return nil, ErrRoomNotFound
	}
	return r, nil
}

// State returns the room's current (redacted while voting) state.
func (s *Service) State(id string) (State, error) {
	r, err := s.room(id)
	if err != nil {
		return State{}, err
	}
	return r.Snapshot(s.now()), nil
}

// Join adds a participant to a room.
func (s *Service) Join(roomID, name, kind string) (pid, token string, err error) {
	r, err := s.room(roomID)
	if err != nil {
		return "", "", err
	}
	pid, token, err = r.Join(name, kind, s.now())
	if err != nil {
		return "", "", err
	}
	s.log.Info("participant_joined", "room_id", roomID, "kind", kind)
	return pid, token, nil
}

// CastVote records a blind vote and reports the voter's kind.
func (s *Service) CastVote(roomID, token, value, rationale string) (kind string, err error) {
	r, err := s.room(roomID)
	if err != nil {
		return "", err
	}
	kind, revealed, err := r.CastVote(token, value, rationale, s.now())
	if err != nil {
		return "", err
	}
	s.log.Info("vote_cast", "room_id", roomID, "kind", kind)
	if revealed != nil {
		s.logRevealed(roomID, revealed)
	}
	return kind, nil
}

// Reveal flips the current round to revealed.
func (s *Service) Reveal(roomID, token string) (State, error) {
	r, err := s.room(roomID)
	if err != nil {
		return State{}, err
	}
	st, err := r.Reveal(token, s.now())
	if err != nil {
		return State{}, err
	}
	s.logRevealed(roomID, &st)
	return st, nil
}

// StartRound archives the revealed round and opens the next.
func (s *Service) StartRound(roomID, token, subject, context string) (State, error) {
	r, err := s.room(roomID)
	if err != nil {
		return State{}, err
	}
	st, err := r.StartRound(token, subject, context, s.now())
	if err != nil {
		return State{}, err
	}
	s.log.Info("round_started", "room_id", roomID, "seq", st.Round.Seq)
	return st, nil
}

// Leave removes the calling participant.
func (s *Service) Leave(roomID, token string) error {
	r, err := s.room(roomID)
	if err != nil {
		return err
	}
	revealed, err := r.Leave(token, s.now())
	if err != nil {
		return err
	}
	s.log.Info("participant_left", "room_id", roomID)
	if revealed != nil {
		s.logRevealed(roomID, revealed)
	}
	return nil
}

// Subscribe streams room events until cancel is called or the room expires.
func (s *Service) Subscribe(roomID string) (<-chan Event, func(), error) {
	r, err := s.room(roomID)
	if err != nil {
		return nil, nil, err
	}
	ch, cancel := r.Subscribe()
	return ch, cancel, nil
}

// RoomCount reports live rooms, for /healthz.
func (s *Service) RoomCount() int { return s.store.Count() }

// RunGC sweeps idle rooms every interval until ctx is done (PLAN.md §3:
// 2h idle TTL, sweep every minute).
func (s *Service) RunGC(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.SweepExpired()
		}
	}
}

// SweepExpired removes rooms idle past the TTL, closing their streams.
func (s *Service) SweepExpired() {
	for _, r := range s.store.Expire(s.now().Add(-RoomTTL)) {
		r.Close()
		s.log.Info("room_expired", "room_id", r.ID())
	}
}

func (s *Service) logRevealed(roomID string, st *State) {
	if st.Results == nil {
		return
	}
	var spread any
	if st.Results.Stats.Spread != nil {
		spread = *st.Results.Stats.Spread
	}
	s.log.Info("revealed",
		"room_id", roomID,
		"spread", spread,
		"consensus", st.Results.Stats.Consensus,
		"n_votes", len(st.Results.Votes),
	)
}
