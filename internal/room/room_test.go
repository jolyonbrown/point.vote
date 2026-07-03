package room

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

func fibRoom(t *testing.T, autoReveal bool) *Room {
	t.Helper()
	deck, err := ResolvePreset("fibonacci")
	if err != nil {
		t.Fatalf("ResolvePreset: %v", err)
	}
	return NewRoom("mint-otter-42", deck, "Add OAuth token refresh", "the ticket body", autoReveal, t0)
}

func join(t *testing.T, r *Room, name, kind string) (pid, token string) {
	t.Helper()
	pid, token, err := r.Join(name, kind, t0)
	if err != nil {
		t.Fatalf("Join(%s, %s): %v", name, kind, err)
	}
	return pid, token
}

func TestValidateDeck(t *testing.T) {
	long := strings.Repeat("x", MaxOptionLen+1)
	tests := []struct {
		name    string
		deck    []string
		wantErr bool
	}{
		{"valid custom", []string{"postgres", "sqlite", "dynamo"}, false},
		{"minimum size", []string{"yes", "no"}, false},
		{"too few", []string{"solo"}, true},
		{"too many", make([]string, MaxDeckSize+1), true},
		{"blank option", []string{"a", " "}, true},
		{"duplicate option", []string{"a", "b", "a"}, true},
		{"option too long", []string{"a", long}, true},
		{"unicode ok", []string{"☕", "?"}, false},
	}
	for i := range tests[3].deck {
		tests[3].deck[i] = strings.Repeat("o", i+1) // unique, non-blank
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDeck(tt.deck)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDeck(%v) error = %v, wantErr %v", tt.deck, err, tt.wantErr)
			}
		})
	}
	for name := range Presets {
		if err := ValidateDeck(Presets[name]); err != nil {
			t.Errorf("preset %q fails its own validation: %v", name, err)
		}
	}
}

func TestJoinValidation(t *testing.T) {
	tests := []struct {
		name    string
		pname   string
		kind    string
		wantErr bool
	}{
		{"human ok", "Alice", KindHuman, false},
		{"agent ok", "claude-code", KindAgent, false},
		{"observer ok", "orchestrator", KindObserver, false},
		{"empty name", "", KindHuman, true},
		{"whitespace name", "   ", KindHuman, true},
		{"name too long", strings.Repeat("a", MaxNameLen+1), KindHuman, true},
		{"name at limit", strings.Repeat("é", MaxNameLen), KindHuman, false},
		{"bad kind", "Alice", "robot", true},
		{"empty kind", "Alice", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := fibRoom(t, true)
			_, _, err := r.Join(tt.pname, tt.kind, t0)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Join(%q, %q) error = %v, wantErr %v", tt.pname, tt.kind, err, tt.wantErr)
			}
		})
	}
}

func TestJoinRoomFull(t *testing.T) {
	r := fibRoom(t, false)
	for i := 0; i < MaxParticipants; i++ {
		join(t, r, "p", KindHuman)
	}
	if _, _, err := r.Join("late", KindHuman, t0); err != ErrRoomFull {
		t.Fatalf("join #%d error = %v, want ErrRoomFull", MaxParticipants+1, err)
	}
}

func TestVoteRules(t *testing.T) {
	t.Run("value not in deck", func(t *testing.T) {
		r := fibRoom(t, false)
		_, token := join(t, r, "Alice", KindHuman)
		var verr ValidationError
		if _, _, err := r.CastVote(token, "4", "", t0); err == nil || !asValidation(err, &verr) {
			t.Fatalf("vote off-deck error = %v, want ValidationError", err)
		}
	})

	t.Run("observer cannot vote", func(t *testing.T) {
		r := fibRoom(t, false)
		_, token := join(t, r, "watcher", KindObserver)
		if _, _, err := r.CastVote(token, "5", "", t0); err != ErrObserverVote {
			t.Fatalf("observer vote error = %v, want ErrObserverVote", err)
		}
	})

	t.Run("bad token", func(t *testing.T) {
		r := fibRoom(t, false)
		join(t, r, "Alice", KindHuman)
		if _, _, err := r.CastVote("nonsense", "5", "", t0); err != ErrBadToken {
			t.Fatalf("bad token error = %v, want ErrBadToken", err)
		}
		if _, _, err := r.CastVote("", "5", "", t0); err != ErrBadToken {
			t.Fatalf("empty token error = %v, want ErrBadToken", err)
		}
	})

	t.Run("rationale too long", func(t *testing.T) {
		r := fibRoom(t, false)
		_, token := join(t, r, "Alice", KindHuman)
		var verr ValidationError
		if _, _, err := r.CastVote(token, "5", strings.Repeat("r", MaxRationaleLen+1), t0); err == nil || !asValidation(err, &verr) {
			t.Fatalf("long rationale error = %v, want ValidationError", err)
		}
	})

	t.Run("re-vote last write wins", func(t *testing.T) {
		r := fibRoom(t, false)
		_, token := join(t, r, "Alice", KindHuman)
		mustVote(t, r, token, "5", "first thoughts")
		mustVote(t, r, token, "13", "second thoughts")
		st, err := r.Reveal(token, t0)
		if err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if n := len(st.Results.Votes); n != 1 {
			t.Fatalf("got %d votes, want 1", n)
		}
		if v := st.Results.Votes[0]; v.Value != "13" || v.Rationale != "second thoughts" {
			t.Fatalf("got vote %+v, want value 13 / second thoughts", v)
		}
	})

	t.Run("vote after reveal is 409", func(t *testing.T) {
		r := fibRoom(t, false)
		_, token := join(t, r, "Alice", KindHuman)
		mustVote(t, r, token, "5", "")
		if _, err := r.Reveal(token, t0); err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if _, _, err := r.CastVote(token, "8", "", t0); err != ErrWrongState {
			t.Fatalf("vote after reveal error = %v, want ErrWrongState", err)
		}
	})
}

func mustVote(t *testing.T, r *Room, token, value, rationale string) *State {
	t.Helper()
	_, revealed, err := r.CastVote(token, value, rationale, t0)
	if err != nil {
		t.Fatalf("CastVote(%s): %v", value, err)
	}
	return revealed
}

func asValidation(err error, target *ValidationError) bool {
	v, ok := err.(ValidationError)
	if ok {
		*target = v
	}
	return ok
}

func TestAutoReveal(t *testing.T) {
	t.Run("fires when last non-observer votes", func(t *testing.T) {
		r := fibRoom(t, true)
		_, alice := join(t, r, "Alice", KindHuman)
		_, claude := join(t, r, "claude", KindAgent)
		join(t, r, "watcher", KindObserver)

		if revealed := mustVote(t, r, alice, "5", ""); revealed != nil {
			t.Fatal("revealed after first of two votes")
		}
		revealed := mustVote(t, r, claude, "13", "")
		if revealed == nil {
			t.Fatal("not revealed after last vote")
		}
		if revealed.Round.State != StateRevealed || revealed.Results == nil {
			t.Fatalf("state %q, results %v — want revealed with results", revealed.Round.State, revealed.Results)
		}
	})

	t.Run("late joiner keeps the round open", func(t *testing.T) {
		r := fibRoom(t, true)
		_, alice := join(t, r, "Alice", KindHuman)
		mustVoteOpen := mustVote(t, r, alice, "5", "") // sole voter → reveals
		if mustVoteOpen == nil {
			t.Fatal("sole voter should trigger auto-reveal")
		}
	})

	t.Run("joiner before last vote prevents reveal", func(t *testing.T) {
		r := fibRoom(t, true)
		_, alice := join(t, r, "Alice", KindHuman)
		_, bob := join(t, r, "Bob", KindHuman)
		mustVote(t, r, alice, "5", "")
		join(t, r, "Carol", KindHuman)
		if revealed := mustVote(t, r, bob, "8", ""); revealed != nil {
			t.Fatal("revealed while Carol had not voted")
		}
	})

	t.Run("off means manual only", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		if revealed := mustVote(t, r, alice, "5", ""); revealed != nil {
			t.Fatal("auto-revealed with auto_reveal off")
		}
		if st := r.Snapshot(t0); st.Round.State != StateVoting {
			t.Fatalf("state %q, want voting", st.Round.State)
		}
	})
}

func TestLeave(t *testing.T) {
	t.Run("departure completes auto-reveal", func(t *testing.T) {
		r := fibRoom(t, true)
		_, alice := join(t, r, "Alice", KindHuman)
		_, bob := join(t, r, "Bob", KindHuman)
		mustVote(t, r, alice, "5", "")
		revealed, err := r.Leave(bob, t0)
		if err != nil {
			t.Fatalf("Leave: %v", err)
		}
		if revealed == nil {
			t.Fatal("expected auto-reveal when the only non-voter left")
		}
	})

	t.Run("voter leaving takes their vote", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		_, bob := join(t, r, "Bob", KindHuman)
		mustVote(t, r, alice, "5", "")
		if _, err := r.Leave(alice, t0); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		st, err := r.Reveal(bob, t0)
		if err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if n := len(st.Results.Votes); n != 0 {
			t.Fatalf("got %d votes after voter left, want 0", n)
		}
	})

	t.Run("last participant leaving does not panic or reveal empty", func(t *testing.T) {
		r := fibRoom(t, true)
		_, alice := join(t, r, "Alice", KindHuman)
		if _, err := r.Leave(alice, t0); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		if st := r.Snapshot(t0); st.Round.State != StateVoting {
			t.Fatalf("empty room state %q, want voting", st.Round.State)
		}
	})
}

func TestRevealRules(t *testing.T) {
	r := fibRoom(t, false)
	_, alice := join(t, r, "Alice", KindHuman)
	if _, err := r.Reveal("bogus", t0); err != ErrBadToken {
		t.Fatalf("reveal with bad token error = %v, want ErrBadToken", err)
	}
	if _, err := r.Reveal(alice, t0); err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if _, err := r.Reveal(alice, t0); err != ErrWrongState {
		t.Fatalf("double reveal error = %v, want ErrWrongState", err)
	}
}

func TestStartRound(t *testing.T) {
	t.Run("requires revealed round", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		if _, err := r.StartRound(alice, "next", "", t0); err != ErrWrongState {
			t.Fatalf("StartRound while voting error = %v, want ErrWrongState", err)
		}
	})

	t.Run("archives summary and increments seq", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		mustVote(t, r, alice, "8", "gut feel")
		if _, err := r.Reveal(alice, t0); err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		st, err := r.StartRound(alice, "round two", "more context", t0)
		if err != nil {
			t.Fatalf("StartRound: %v", err)
		}
		if st.Round.Seq != 2 || st.Round.State != StateVoting || st.Round.Subject != "round two" {
			t.Fatalf("round = %+v, want seq 2 voting 'round two'", st.Round)
		}
		if st.Results != nil {
			t.Fatal("fresh round leaked previous results")
		}
		if len(st.History) != 1 {
			t.Fatalf("history len %d, want 1", len(st.History))
		}
		h := st.History[0]
		if h.Seq != 1 || h.Subject != "Add OAuth token refresh" || len(h.Votes) != 1 || h.Votes[0].Value != "8" || h.Votes[0].Rationale != "gut feel" {
			t.Fatalf("archived summary = %+v", h)
		}
	})

	t.Run("history is a 20-round ring", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		for i := 0; i < HistoryLen+5; i++ {
			mustVote(t, r, alice, "5", "")
			if _, err := r.Reveal(alice, t0); err != nil {
				t.Fatalf("Reveal round %d: %v", i+1, err)
			}
			if _, err := r.StartRound(alice, "", "", t0); err != nil {
				t.Fatalf("StartRound %d: %v", i+1, err)
			}
		}
		st := r.Snapshot(t0)
		if len(st.History) != HistoryLen {
			t.Fatalf("history len %d, want %d", len(st.History), HistoryLen)
		}
		if oldest := st.History[0].Seq; oldest != 6 {
			t.Fatalf("oldest archived seq %d, want 6", oldest)
		}
	})
}

// TestRedaction is the critical test: while a round is voting, no read path
// may expose vote values or rationales — not the snapshot, not events.
func TestRedaction(t *testing.T) {
	const canary = "SUPER-SECRET-RATIONALE-CANARY"
	r := fibRoom(t, true)
	_, alice := join(t, r, "Alice", KindHuman)
	_, claude := join(t, r, "claude", KindAgent)

	ch, cancel := r.Subscribe()
	defer cancel()

	mustVote(t, r, alice, "13", canary)

	st := r.Snapshot(t0)
	if st.Results != nil {
		t.Fatal("results non-nil while voting")
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), canary) {
		t.Fatalf("voting-state snapshot leaked the rationale: %s", raw)
	}
	var ps ParticipantState
	for _, p := range st.Round.Participants {
		if p.Name == "Alice" {
			ps = p
		}
	}
	if !ps.HasVoted {
		t.Fatal("has_voted not set for Alice")
	}
	if st.Round.VotesCast != 1 {
		t.Fatalf("votes_cast = %d, want 1", st.Round.VotesCast)
	}

	// Every event broadcast while voting must be redacted too.
	drainEvents(t, ch, func(ev Event) {
		if ev.State.Round.State != StateVoting {
			return
		}
		raw, _ := json.Marshal(ev.State)
		if strings.Contains(string(raw), canary) {
			t.Fatalf("event %q leaked the rationale while voting", ev.Name)
		}
	})

	// After reveal, values and rationales are visible.
	revealed := mustVote(t, r, claude, "5", "counterpoint")
	if revealed == nil {
		t.Fatal("expected auto-reveal")
	}
	raw, _ = json.Marshal(revealed)
	if !strings.Contains(string(raw), canary) {
		t.Fatal("revealed state missing the rationale")
	}
}

func drainEvents(t *testing.T, ch <-chan Event, f func(Event)) {
	t.Helper()
	for {
		select {
		case ev := <-ch:
			f(ev)
		default:
			return
		}
	}
}

func TestSubscribeEventNames(t *testing.T) {
	r := fibRoom(t, true)
	_, alice := join(t, r, "Alice", KindHuman)

	ch, cancel := r.Subscribe()
	defer cancel()

	_, bob := join(t, r, "Bob", KindHuman)                     // joined
	mustVote(t, r, alice, "5", "")                             // voted
	mustVote(t, r, bob, "8", "")                               // voted + revealed
	if _, err := r.StartRound(alice, "", "", t0); err != nil { // round_started
		t.Fatalf("StartRound: %v", err)
	}
	if _, err := r.Leave(bob, t0); err != nil { // left (+revealed: alice has no vote yet → no)
		t.Fatalf("Leave: %v", err)
	}

	var names []string
	drainEvents(t, ch, func(ev Event) { names = append(names, ev.Name) })
	want := []string{"joined", "voted", "voted", "revealed", "round_started", "left"}
	if len(names) != len(want) {
		t.Fatalf("got events %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q (all: %v)", i, names[i], want[i], names)
		}
	}
	lastID := 0
	// IDs must be monotonically increasing for Last-Event-ID.
	drainEvents(t, ch, func(ev Event) {
		if ev.ID <= lastID {
			t.Fatalf("event IDs not increasing: %d after %d", ev.ID, lastID)
		}
		lastID = ev.ID
	})
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	r := fibRoom(t, false)
	_, alice := join(t, r, "Alice", KindHuman)
	_, cancel := r.Subscribe() // never read
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < subBuffer*3; i++ {
			if _, _, err := r.CastVote(alice, "5", "", t0); err != nil {
				t.Errorf("CastVote: %v", err)
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("broadcast blocked on a slow subscriber")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := fibRoom(t, false)
	tokens := make([]string, 8)
	for i := range tokens {
		_, tokens[i] = join(t, r, "voter", KindHuman)
	}
	var wg sync.WaitGroup
	for _, token := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _, _ = r.CastVote(token, "5", "", t0)
				_ = r.Snapshot(t0)
			}
		}()
	}
	ch, cancel := r.Subscribe()
	go func() {
		for range ch {
		}
	}()
	wg.Wait()
	cancel()
}
