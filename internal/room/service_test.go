package room

import (
	"log/slog"
	"testing"
	"time"
)

func testService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	now := t0
	svc := NewService(NewMemStore(), slog.New(slog.DiscardHandler))
	svc.now = func() time.Time { return now }
	return svc, &now
}

func createFib(t *testing.T, svc *Service) State {
	t.Helper()
	deck, _ := ResolvePreset(DefaultPreset)
	st, err := svc.CreateRoom(deck, "subject", "", true)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	return st
}

func TestServiceCreateRoomValidates(t *testing.T) {
	svc, _ := testService(t)
	if _, err := svc.CreateRoom([]string{"solo"}, "", "", true); err == nil {
		t.Fatal("bad deck accepted")
	}
	if _, err := svc.CreateRoom([]string{"a", "b"}, string(make([]rune, MaxSubjectLen+1)), "", true); err == nil {
		t.Fatal("long subject accepted")
	}
}

func TestServiceCreateRoomRetriesCollisions(t *testing.T) {
	svc, _ := testService(t)
	ids := []string{"same-id-01", "same-id-01", "other-id-02"}
	i := 0
	svc.genID = func() string { id := ids[i]; i++; return id }

	deck, _ := ResolvePreset(DefaultPreset)
	first, err := svc.CreateRoom(deck, "", "", true)
	if err != nil {
		t.Fatalf("first CreateRoom: %v", err)
	}
	second, err := svc.CreateRoom(deck, "", "", true)
	if err != nil {
		t.Fatalf("second CreateRoom: %v", err)
	}
	if first.RoomID != "same-id-01" || second.RoomID != "other-id-02" {
		t.Fatalf("ids = %q, %q — collision not retried", first.RoomID, second.RoomID)
	}
}

func TestServiceTTLExpiry(t *testing.T) {
	svc, now := testService(t)
	st := createFib(t, svc)

	ch, cancel, err := svc.Subscribe(st.RoomID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// Just under the TTL: still alive.
	*now = t0.Add(RoomTTL - time.Second)
	svc.SweepExpired()
	if _, err := svc.State(st.RoomID); err != nil {
		t.Fatalf("room expired before TTL: %v", err)
	}
	// The State call above touched the room, so the TTL restarts from here.
	*now = now.Add(RoomTTL + time.Second)
	svc.SweepExpired()
	if _, err := svc.State(st.RoomID); err != ErrRoomNotFound {
		t.Fatalf("State after expiry error = %v, want ErrRoomNotFound", err)
	}
	if svc.RoomCount() != 0 {
		t.Fatalf("RoomCount = %d after expiry, want 0", svc.RoomCount())
	}

	// The subscriber stream must be closed by expiry.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed, as required
			}
		case <-deadline:
			t.Fatal("subscriber channel not closed on room expiry")
		}
	}
}

func TestServiceActivityDefersExpiry(t *testing.T) {
	svc, now := testService(t)
	st := createFib(t, svc)

	// Keep touching the room at half-TTL intervals; it must survive.
	for i := 0; i < 5; i++ {
		*now = now.Add(RoomTTL / 2)
		svc.SweepExpired()
		if _, err := svc.State(st.RoomID); err != nil {
			t.Fatalf("active room expired on touch %d: %v", i, err)
		}
	}
}

func TestMemStoreCaps(t *testing.T) {
	s := NewMemStore()
	deck := []string{"yes", "no"}
	for i := 0; i < MaxRooms; i++ {
		r := NewRoom(idFor(i), deck, "", "", true, t0)
		if err := s.Put(r); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	if err := s.Put(NewRoom("one-too-many-00", deck, "", "", true, t0)); err != ErrServerFull {
		t.Fatalf("Put over cap error = %v, want ErrServerFull", err)
	}
	if err := s.Put(NewRoom(idFor(0), deck, "", "", true, t0)); err != ErrIDTaken {
		t.Fatalf("duplicate Put error = %v, want ErrIDTaken", err)
	}
}

func idFor(i int) string {
	return string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)) + "-room-01"
}
