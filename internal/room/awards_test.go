package room

import (
	"slices"
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

	t.Run("settlement survives into later snapshots", func(t *testing.T) {
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
		if st.Settled == nil || st.Settled.Value != "5" {
			t.Fatalf("settlement lost after new round: %+v", st.Settled)
		}
	})
}
