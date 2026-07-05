package room

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func summaryOf(seq int, deck []string, pairs ...string) RoundSummary {
	var vs []RevealedVote
	for i := 0; i+1 < len(pairs); i += 2 {
		vs = append(vs, RevealedVote{Name: pairs[i], Kind: KindHuman, Value: pairs[i+1]})
	}
	return RoundSummary{Seq: seq, Votes: vs, Stats: computeStats(vs, deck)}
}

func awardByID(t *testing.T, awards []Award, id string) *Award {
	t.Helper()
	for i := range awards {
		if awards[i].ID == id {
			return &awards[i]
		}
	}
	return nil
}

func TestComputeAwards(t *testing.T) {
	fib, _ := ResolvePreset("fibonacci")

	t.Run("sole voter exact hit: oracle only", func(t *testing.T) {
		awards := computeAwards([]RoundSummary{summaryOf(1, fib, "solo", "8")}, fib, "8")
		if len(awards) != 1 {
			t.Fatalf("awards = %+v, want oracle only", awards)
		}
		oracle := awardByID(t, awards, "oracle")
		if oracle == nil || !slices.Equal(oracle.Names, []string{"solo"}) {
			t.Fatalf("oracle = %+v", oracle)
		}
	})

	t.Run("equidistant first votes share oracle, no outlier", func(t *testing.T) {
		// 5 and 13 are both one deck step from 8.
		rounds := []RoundSummary{
			summaryOf(1, fib, "a", "5", "b", "13"),
			summaryOf(2, fib, "a", "8", "b", "8"),
		}
		awards := computeAwards(rounds, fib, "8")
		oracle := awardByID(t, awards, "oracle")
		if oracle == nil || len(oracle.Names) != 2 {
			t.Fatalf("oracle = %+v, want shared", oracle)
		}
		if saw := awardByID(t, awards, "saw_something"); saw != nil {
			t.Fatalf("saw_something awarded with no outlier: %+v", saw)
		}
		diplomat := awardByID(t, awards, "diplomat")
		if diplomat == nil || len(diplomat.Names) != 2 {
			t.Fatalf("diplomat = %+v, want both movers", diplomat)
		}
		if opt := awardByID(t, awards, "optimist"); opt == nil || !slices.Equal(opt.Names, []string{"a"}) {
			t.Fatalf("optimist = %+v, want a", opt)
		}
		if pess := awardByID(t, awards, "pessimist"); pess == nil || !slices.Equal(pess.Names, []string{"b"}) {
			t.Fatalf("pessimist = %+v, want b", pess)
		}
	})

	t.Run("clear oracle and outlier", func(t *testing.T) {
		// settled 8: b's 13 is 1 step away, a's 3 is 2 steps away.
		rounds := []RoundSummary{
			summaryOf(1, fib, "a", "3", "b", "13"),
			summaryOf(2, fib, "a", "8", "b", "8"),
		}
		awards := computeAwards(rounds, fib, "8")
		if oracle := awardByID(t, awards, "oracle"); oracle == nil || !slices.Equal(oracle.Names, []string{"b"}) {
			t.Fatalf("oracle = %+v, want b", oracle)
		}
		saw := awardByID(t, awards, "saw_something")
		if saw == nil || !slices.Equal(saw.Names, []string{"a"}) {
			t.Fatalf("saw_something = %+v, want a", saw)
		}
	})

	t.Run("non-numeric deck: no optimist or pessimist", func(t *testing.T) {
		yn, _ := ResolvePreset("yesno")
		rounds := []RoundSummary{
			summaryOf(1, yn, "a", "yes", "b", "no"),
			summaryOf(2, yn, "a", "yes", "b", "yes"),
		}
		awards := computeAwards(rounds, yn, "yes")
		if oracle := awardByID(t, awards, "oracle"); oracle == nil || !slices.Equal(oracle.Names, []string{"a"}) {
			t.Fatalf("oracle = %+v, want a", oracle)
		}
		if saw := awardByID(t, awards, "saw_something"); saw == nil || !slices.Equal(saw.Names, []string{"b"}) {
			t.Fatalf("saw_something = %+v, want b", saw)
		}
		if diplomat := awardByID(t, awards, "diplomat"); diplomat == nil || !slices.Equal(diplomat.Names, []string{"b"}) {
			t.Fatalf("diplomat = %+v, want b", diplomat)
		}
		if awardByID(t, awards, "optimist") != nil || awardByID(t, awards, "pessimist") != nil {
			t.Fatalf("direction awards on a non-numeric deck: %+v", awards)
		}
	})

	t.Run("escape cards cannot win accuracy awards on numeric settles", func(t *testing.T) {
		// carol's "?" would be deck-adjacent to nothing sensible; she
		// competes for no accuracy award. a and b are equidistant from 8.
		rounds := []RoundSummary{summaryOf(1, fib, "a", "5", "b", "13", "carol", "?")}
		awards := computeAwards(rounds, fib, "8")
		oracle := awardByID(t, awards, "oracle")
		if oracle == nil || slices.Contains(oracle.Names, "carol") || len(oracle.Names) != 2 {
			t.Fatalf("oracle = %+v, want a and b only", oracle)
		}
		if saw := awardByID(t, awards, "saw_something"); saw != nil {
			t.Fatalf("saw_something = %+v, want none (numeric votes were equidistant)", saw)
		}
	})

	t.Run("tied outliers on the same card read cleanly", func(t *testing.T) {
		rounds := []RoundSummary{summaryOf(1, fib, "a", "0", "b", "0", "c", "8")}
		awards := computeAwards(rounds, fib, "8")
		saw := awardByID(t, awards, "saw_something")
		if saw == nil || len(saw.Names) != 2 {
			t.Fatalf("saw_something = %+v, want a and b", saw)
		}
		if strings.Contains(saw.Detail, "0/0") {
			t.Fatalf("detail duplicates the shared value: %q", saw.Detail)
		}
	})

	t.Run("single round means no diplomat", func(t *testing.T) {
		awards := computeAwards([]RoundSummary{summaryOf(1, fib, "a", "5", "b", "8")}, fib, "8")
		if awardByID(t, awards, "diplomat") != nil {
			t.Fatal("diplomat awarded with only one round")
		}
	})

	t.Run("no rounds, no awards", func(t *testing.T) {
		if awards := computeAwards(nil, fib, "8"); len(awards) != 0 {
			t.Fatalf("awards = %+v, want none", awards)
		}
	})
}

func TestSettle(t *testing.T) {
	t.Run("requires revealed round", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		if _, err := r.Settle(alice, "8", t0); err != ErrWrongState {
			t.Fatalf("settle while voting error = %v, want ErrWrongState", err)
		}
	})

	t.Run("value must be in deck", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		mustVote(t, r, alice, "5", "")
		if _, err := r.Reveal(alice, t0); err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		var verr ValidationError
		if _, err := r.Settle(alice, "4", t0); err == nil || !asValidation(err, &verr) {
			t.Fatalf("off-deck settle error = %v, want ValidationError", err)
		}
	})

	t.Run("bad token", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		mustVote(t, r, alice, "5", "")
		if _, err := r.Reveal(alice, t0); err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if _, err := r.Settle("bogus", "5", t0); err != ErrBadToken {
			t.Fatalf("settle with bad token error = %v, want ErrBadToken", err)
		}
	})

	t.Run("observer may settle and re-settle overwrites", func(t *testing.T) {
		r := fibRoom(t, true)
		_, alice := join(t, r, "Alice", KindHuman)
		_, watcher := join(t, r, "orchestrator", KindObserver)

		ch, cancel := r.Subscribe()
		defer cancel()

		mustVote(t, r, alice, "5", "") // sole voter → auto-reveal

		st, err := r.Settle(watcher, "5", t0)
		if err != nil {
			t.Fatalf("observer settle: %v", err)
		}
		if st.Settled == nil || st.Settled.Value != "5" || st.Settled.By != "orchestrator" {
			t.Fatalf("settled = %+v", st.Settled)
		}
		if len(st.Settled.Awards) == 0 {
			t.Fatal("no awards computed")
		}

		var sawSettled bool
		drainEvents(t, ch, func(ev Event) {
			if ev.Name == "settled" {
				sawSettled = true
				if ev.State.Settled == nil {
					t.Error("settled event carries no settlement")
				}
			}
		})
		if !sawSettled {
			t.Fatal("no settled event broadcast")
		}

		st, err = r.Settle(watcher, "8", t0.Add(time.Minute))
		if err != nil {
			t.Fatalf("re-settle: %v", err)
		}
		if st.Settled.Value != "8" {
			t.Fatalf("re-settle value = %q, want 8", st.Settled.Value)
		}
	})

	t.Run("settle, next round, reveal, re-settle: no double counting", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		_, bob := join(t, r, "Bob", KindHuman)

		// Round 1: 3 vs 13, revealed, settled on 8.
		mustVote(t, r, alice, "3", "")
		mustVote(t, r, bob, "13", "")
		if _, err := r.Reveal(alice, t0); err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if _, err := r.Settle(alice, "8", t0); err != nil {
			t.Fatalf("Settle 1: %v", err)
		}
		if _, err := r.StartRound(alice, "round 2", "", t0); err != nil {
			t.Fatalf("StartRound: %v", err)
		}

		// Round 2: both on 8, revealed, re-settled.
		mustVote(t, r, alice, "8", "")
		mustVote(t, r, bob, "8", "")
		if _, err := r.Reveal(alice, t0); err != nil {
			t.Fatalf("Reveal 2: %v", err)
		}
		st, err := r.Settle(bob, "8", t0)
		if err != nil {
			t.Fatalf("Settle 2: %v", err)
		}

		// Awards must see exactly rounds 1 and 2 once each: the diplomat
		// exists (two distinct rounds), and the optimist/pessimist nets
		// reflect single-counted round-1 votes (alice -2, bob +1).
		if st.Settled.By != "Bob" || st.Settled.Value != "8" {
			t.Fatalf("settled = %+v", st.Settled)
		}
		diplomat := awardByID(t, st.Settled.Awards, "diplomat")
		if diplomat == nil || len(diplomat.Names) != 2 {
			t.Fatalf("diplomat = %+v, want both movers", diplomat)
		}
		if opt := awardByID(t, st.Settled.Awards, "optimist"); opt == nil || !slices.Equal(opt.Names, []string{"Alice"}) {
			t.Fatalf("optimist = %+v, want Alice only", opt)
		}
		if pess := awardByID(t, st.Settled.Awards, "pessimist"); pess == nil || !slices.Equal(pess.Names, []string{"Bob"}) {
			t.Fatalf("pessimist = %+v, want Bob only", pess)
		}
		if oracle := awardByID(t, st.Settled.Awards, "oracle"); oracle == nil || !slices.Equal(oracle.Names, []string{"Bob"}) {
			t.Fatalf("oracle = %+v, want Bob (13 is one step from 8, 3 is two)", oracle)
		}
	})

	t.Run("settlement archives onto the concluded round", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		mustVote(t, r, alice, "5", "")
		if _, err := r.Reveal(alice, t0); err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if _, err := r.Settle(alice, "5", t0); err != nil {
			t.Fatalf("Settle: %v", err)
		}
		if _, err := r.StartRound(alice, "aftermath", "", t0); err != nil {
			t.Fatalf("StartRound: %v", err)
		}
		st := r.Snapshot(t0)
		if st.Settled != nil {
			t.Fatalf("live settlement should clear when the next round starts: %+v", st.Settled)
		}
		if len(st.History) != 1 || st.History[0].Called != "5" {
			t.Fatalf("history[0].Called = %+v, want \"5\"", st.History)
		}
	})

	t.Run("unsettled rounds archive without a call", func(t *testing.T) {
		r := fibRoom(t, false)
		_, alice := join(t, r, "Alice", KindHuman)
		mustVote(t, r, alice, "5", "")
		if _, err := r.Reveal(alice, t0); err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if _, err := r.StartRound(alice, "", "", t0); err != nil {
			t.Fatalf("StartRound: %v", err)
		}
		if st := r.Snapshot(t0); st.History[0].Called != "" {
			t.Fatalf("history[0].Called = %q, want empty", st.History[0].Called)
		}
	})
}
