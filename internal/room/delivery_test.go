package room

import (
	"testing"
	"time"
)

func TestSnapshotRevisionMatchesEvent(t *testing.T) {
	r := fibRoom(t, true)
	ch, cancel := r.Subscribe()
	defer cancel()
	_, token := join(t, r, "Alice", KindHuman)
	joined := <-ch
	before := r.Snapshot(t0)
	if before.Revision != joined.ID || joined.State.Revision != joined.ID {
		t.Fatal("join snapshot and event revisions differ")
	}
	mustVote(t, r, token, "5", "secret")
	voted, revealed := <-ch, <-ch
	after := r.Snapshot(t0)
	if voted.State.Results != nil || revealed.State.Results == nil {
		t.Fatal("revisioned events broke atomic reveal/redaction")
	}
	if voted.State.Revision <= before.Revision ||
		revealed.State.Revision <= voted.State.Revision ||
		revealed.State.Revision != revealed.ID ||
		after.Revision != revealed.State.Revision {
		t.Fatal("state revisions did not track mutations in order")
	}
	if before.Revision != joined.ID || before.Results != nil {
		t.Fatal("publishing later events mutated an earlier snapshot")
	}
	if r.Snapshot(t0.Add(time.Second)).Revision != after.Revision {
		t.Fatal("reading state changed the revision")
	}
}

func TestSlowSubscriberReceivesFinalReveal(t *testing.T) {
	r := fibRoom(t, false)
	_, token := join(t, r, "Alice", KindHuman)
	ch, cancel := r.Subscribe()
	defer cancel()
	for range subBuffer {
		mustVote(t, r, token, "5", "hidden until reveal")
	}
	// Transient reactions must not displace queued state.
	if err := r.React(token, "🎉", t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(ch) != subBuffer {
		t.Fatalf("reaction changed a full queue: %d", len(ch))
	}
	final, err := r.Reveal(token, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch) != 1 {
		t.Fatalf("expected coalesced latest snapshot, got %d events", len(ch))
	}
	ev := <-ch
	if ev.Name != "revealed" || ev.State.Results == nil ||
		ev.State.Revision != final.Revision {
		t.Fatalf("slow subscriber missed final reveal: %#v", ev)
	}
	// The subscription still works after overflow.
	next, err := r.StartRound(token, "Next", "", t0)
	if err != nil {
		t.Fatal(err)
	}
	if ev := <-ch; ev.State.Revision != next.Revision || ev.State.Results != nil {
		t.Fatal("subscription did not receive the next redacted round")
	}
}
