// Package room holds the domain: rooms, rounds, votes, reveal rules, and
// stats, behind a Store interface. The one rule that matters most: vote
// values and rationales are never exposed while a round is in the voting
// state — snapshots are structurally redacted.
package room

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Limits from PLAN.md §3 — constants, enforced, tested.
const (
	MaxParticipants = 100
	MaxRooms        = 10_000
	RoomTTL         = 2 * time.Hour
	GCInterval      = time.Minute
	HistoryLen      = 20

	MinDeckSize  = 2
	MaxDeckSize  = 26
	MaxOptionLen = 24

	MaxNameLen      = 40
	MaxSubjectLen   = 200
	MaxContextLen   = 4 * 1024
	MaxRationaleLen = 500
)

// Participant kinds. Observers watch; only humans and agents vote.
const (
	KindHuman    = "human"
	KindAgent    = "agent"
	KindObserver = "observer"
)

// Round states.
const (
	StateVoting   = "voting"
	StateRevealed = "revealed"
)

// Domain errors, mapped to HTTP status codes by the API layer.
var (
	ErrRoomNotFound = errors.New("no such room")
	ErrBadToken     = errors.New("invalid or missing token")
	ErrWrongState   = errors.New("round is in the wrong state for that")
	ErrRoomFull     = errors.New("room is full")
	ErrServerFull   = errors.New("too many rooms; try again later")
	ErrObserverVote = errors.New("observers cannot vote")
)

// ValidationError marks a bad request field (HTTP 400).
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

func validationf(format string, args ...any) error {
	return ValidationError(fmt.Sprintf(format, args...))
}

// Presets are the built-in decks. A deck is just a list of options;
// estimation is merely the default.
var Presets = map[string][]string{
	"fibonacci": {"0", "1", "2", "3", "5", "8", "13", "21", "?", "☕"},
	"tshirt":    {"XS", "S", "M", "L", "XL", "?"},
	"powers2":   {"1", "2", "4", "8", "16", "?"},
	"yesno":     {"yes", "no", "abstain"},
}

// DefaultPreset is used when room creation names no deck.
const DefaultPreset = "fibonacci"

// ResolvePreset returns a copy of the named preset deck.
func ResolvePreset(name string) ([]string, error) {
	deck, ok := Presets[name]
	if !ok {
		return nil, validationf("unknown deck preset %q", name)
	}
	return slices.Clone(deck), nil
}

// ValidateDeck checks a deck against the §3 limits.
func ValidateDeck(deck []string) error {
	if len(deck) < MinDeckSize || len(deck) > MaxDeckSize {
		return validationf("deck needs %d-%d options, got %d", MinDeckSize, MaxDeckSize, len(deck))
	}
	seen := make(map[string]bool, len(deck))
	for _, opt := range deck {
		if strings.TrimSpace(opt) == "" {
			return validationf("deck options must not be blank")
		}
		if utf8.RuneCountInString(opt) > MaxOptionLen {
			return validationf("deck option %q exceeds %d characters", opt, MaxOptionLen)
		}
		if seen[opt] {
			return validationf("duplicate deck option %q", opt)
		}
		seen[opt] = true
	}
	return nil
}

func validateRoundFields(subject, context string) error {
	if utf8.RuneCountInString(subject) > MaxSubjectLen {
		return validationf("subject must be at most %d characters", MaxSubjectLen)
	}
	if len(context) > MaxContextLen {
		return validationf("context must be at most %d bytes", MaxContextLen)
	}
	return nil
}

// Room is a live planning-poker room. All exported methods are safe for
// concurrent use; each room has its own mutex (PLAN.md §8).
type Room struct {
	id         string
	deck       []string
	autoReveal bool
	createdAt  time.Time

	mu           sync.Mutex
	participants map[string]*Participant
	joinSeq      int
	round        *round
	history      []RoundSummary
	lastActive   time.Time
	subs         map[chan Event]struct{}
	eventSeq     int
	closed       bool
}

// Participant is a member of a room. The raw bearer token is never stored,
// only its SHA-256.
type Participant struct {
	id        string
	name      string
	kind      string
	tokenHash [32]byte
	joinedAt  time.Time
	seq       int // join order, for stable listings
}

type round struct {
	seq       int
	subject   string
	context   string
	state     string
	votes     map[string]Vote // participant ID → vote
	startedAt time.Time
	results   *Results // computed once, at reveal
}

// Vote is a single participant's answer. Hidden until reveal.
type Vote struct {
	Value     string
	Rationale string
	CastAt    time.Time
}

// NewRoom builds a room with round 1 open for voting. The caller validates
// deck, subject and context first.
func NewRoom(id string, deck []string, subject, context string, autoReveal bool, now time.Time) *Room {
	return &Room{
		id:           id,
		deck:         slices.Clone(deck),
		autoReveal:   autoReveal,
		createdAt:    now,
		participants: make(map[string]*Participant),
		round: &round{
			seq:       1,
			subject:   subject,
			context:   context,
			state:     StateVoting,
			votes:     make(map[string]Vote),
			startedAt: now,
		},
		history: []RoundSummary{},
		subs:    make(map[chan Event]struct{}),

		lastActive: now,
	}
}

// ID returns the room's capability identifier.
func (r *Room) ID() string { return r.id }

// LastActiveAt reports when the room last saw a request, for TTL GC.
func (r *Room) LastActiveAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastActive
}

// Join adds a participant and returns their ID and one-time bearer token.
func (r *Room) Join(name, kind string, now time.Time) (pid, token string, err error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameLen {
		return "", "", validationf("name must be 1-%d characters", MaxNameLen)
	}
	switch kind {
	case KindHuman, KindAgent, KindObserver:
	default:
		return "", "", validationf("kind must be %q, %q or %q", KindHuman, KindAgent, KindObserver)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", "", ErrRoomNotFound
	}
	r.lastActive = now
	if len(r.participants) >= MaxParticipants {
		return "", "", ErrRoomFull
	}

	pid = newParticipantID()
	for r.participants[pid] != nil {
		pid = newParticipantID()
	}
	token, hash := newToken()
	r.joinSeq++
	r.participants[pid] = &Participant{
		id:        pid,
		name:      name,
		kind:      kind,
		tokenHash: hash,
		joinedAt:  now,
		seq:       r.joinSeq,
	}
	r.broadcastLocked("joined")
	return pid, token, nil
}

// authLocked resolves a bearer token to a participant.
func (r *Room) authLocked(token string) (*Participant, error) {
	if token == "" {
		return nil, ErrBadToken
	}
	h := hashToken(token)
	for _, p := range r.participants {
		if subtle.ConstantTimeCompare(h[:], p.tokenHash[:]) == 1 {
			return p, nil
		}
	}
	return nil, ErrBadToken
}

// CastVote records (or replaces — last write wins) the caller's vote. If
// auto-reveal is on and this was the last missing vote, the round flips to
// revealed atomically and the revealed state is returned.
func (r *Room) CastVote(token, value, rationale string, now time.Time) (kind string, revealed *State, err error) {
	if utf8.RuneCountInString(rationale) > MaxRationaleLen {
		return "", nil, validationf("rationale must be at most %d characters", MaxRationaleLen)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", nil, ErrRoomNotFound
	}
	r.lastActive = now
	p, err := r.authLocked(token)
	if err != nil {
		return "", nil, err
	}
	if p.kind == KindObserver {
		return "", nil, ErrObserverVote
	}
	if r.round.state != StateVoting {
		return "", nil, ErrWrongState
	}
	if !slices.Contains(r.deck, value) {
		return "", nil, validationf("value %q is not in this room's deck", value)
	}

	r.round.votes[p.id] = Vote{Value: value, Rationale: rationale, CastAt: now}
	r.broadcastLocked("voted")
	if r.autoReveal && r.allVotedLocked() {
		r.revealLocked()
		st := r.snapshotLocked()
		return p.kind, &st, nil
	}
	return p.kind, nil, nil
}

// allVotedLocked reports whether every non-observer has voted. False for a
// room with no eligible voters, so an observers-only room never reveals.
func (r *Room) allVotedLocked() bool {
	n := 0
	for _, p := range r.participants {
		if p.kind == KindObserver {
			continue
		}
		n++
		if _, ok := r.round.votes[p.id]; !ok {
			return false
		}
	}
	return n > 0
}

func (r *Room) revealLocked() {
	r.round.state = StateRevealed
	r.round.results = r.buildResultsLocked()
	r.broadcastLocked("revealed")
}

func (r *Room) buildResultsLocked() *Results {
	votes := make([]RevealedVote, 0, len(r.round.votes))
	for _, p := range r.sortedParticipantsLocked() {
		if v, ok := r.round.votes[p.id]; ok {
			votes = append(votes, RevealedVote{Name: p.name, Kind: p.kind, Value: v.Value, Rationale: v.Rationale})
		}
	}
	return &Results{Votes: votes, Stats: computeStats(votes)}
}

// Reveal flips the round to revealed. Any participant may do it.
func (r *Room) Reveal(token string, now time.Time) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return State{}, ErrRoomNotFound
	}
	r.lastActive = now
	if _, err := r.authLocked(token); err != nil {
		return State{}, err
	}
	if r.round.state != StateVoting {
		return State{}, ErrWrongState
	}
	r.revealLocked()
	return r.snapshotLocked(), nil
}

// StartRound archives the current revealed round and opens the next one.
// The current round must be revealed first: archiving a voting round would
// copy blind votes into history, which is exactly what must never happen.
func (r *Room) StartRound(token, subject, context string, now time.Time) (State, error) {
	if err := validateRoundFields(subject, context); err != nil {
		return State{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return State{}, ErrRoomNotFound
	}
	r.lastActive = now
	if _, err := r.authLocked(token); err != nil {
		return State{}, err
	}
	if r.round.state != StateRevealed {
		return State{}, ErrWrongState
	}

	r.history = append(r.history, RoundSummary{
		Seq:     r.round.seq,
		Subject: r.round.subject,
		Votes:   r.round.results.Votes,
		Stats:   r.round.results.Stats,
	})
	if len(r.history) > HistoryLen {
		r.history = r.history[1:]
	}
	r.round = &round{
		seq:       r.round.seq + 1,
		subject:   subject,
		context:   context,
		state:     StateVoting,
		votes:     make(map[string]Vote),
		startedAt: now,
	}
	r.broadcastLocked("round_started")
	return r.snapshotLocked(), nil
}

// Leave removes the caller and, while voting, their vote. If the departure
// leaves everyone else having voted, auto-reveal may fire.
func (r *Room) Leave(token string, now time.Time) (revealed *State, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrRoomNotFound
	}
	r.lastActive = now
	p, err := r.authLocked(token)
	if err != nil {
		return nil, err
	}
	delete(r.participants, p.id)
	if r.round.state == StateVoting {
		delete(r.round.votes, p.id)
	}
	r.broadcastLocked("left")
	if r.round.state == StateVoting && r.autoReveal && r.allVotedLocked() {
		r.revealLocked()
		st := r.snapshotLocked()
		return &st, nil
	}
	return nil, nil
}

// Snapshot returns the current wire state, redacted while voting.
func (r *Room) Snapshot(now time.Time) State {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastActive = now
	return r.snapshotLocked()
}

// snapshotLocked builds the redacted wire state. Redaction is structural:
// while voting, the state has no field that could carry a value or
// rationale — only has_voted flags and a votes_cast count.
func (r *Room) snapshotLocked() State {
	parts := make([]ParticipantState, 0, len(r.participants))
	for _, p := range r.sortedParticipantsLocked() {
		_, voted := r.round.votes[p.id]
		parts = append(parts, ParticipantState{ID: p.id, Name: p.name, Kind: p.kind, HasVoted: voted})
	}
	st := State{
		RoomID:     r.id,
		Deck:       slices.Clone(r.deck),
		AutoReveal: r.autoReveal,
		Round: RoundState{
			Seq:          r.round.seq,
			Subject:      r.round.subject,
			Context:      r.round.context,
			State:        r.round.state,
			VotesCast:    len(r.round.votes),
			Participants: parts,
		},
		History: slices.Clone(r.history),
	}
	if r.round.state == StateRevealed {
		st.Results = r.round.results
	}
	return st
}

func (r *Room) sortedParticipantsLocked() []*Participant {
	ps := make([]*Participant, 0, len(r.participants))
	for _, p := range r.participants {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].seq < ps[j].seq })
	return ps
}

// subBuffer sizes subscriber channels. Sends are non-blocking: a slow
// consumer just misses a snapshot; the next event carries full state again.
const subBuffer = 8

// Subscribe registers for room events. The channel closes when the room
// expires. Call cancel when done.
func (r *Room) Subscribe() (<-chan Event, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan Event, subBuffer)
	if r.closed {
		close(ch)
		return ch, func() {}
	}
	r.subs[ch] = struct{}{}
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(r.subs, ch)
		})
	}
	return ch, cancel
}

// Close marks the room dead and closes all subscriber channels.
func (r *Room) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for ch := range r.subs {
		close(ch)
	}
	clear(r.subs)
}

func (r *Room) broadcastLocked(name string) {
	if r.closed {
		return
	}
	r.eventSeq++
	ev := Event{ID: r.eventSeq, Name: name, State: r.snapshotLocked()}
	for ch := range r.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
