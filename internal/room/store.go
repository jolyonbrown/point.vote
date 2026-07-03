package room

import (
	"errors"
	"time"
)

// ErrIDTaken reports a room ID collision; the caller regenerates and
// retries.
var ErrIDTaken = errors.New("room id already taken")

// Store owns the live set of rooms. This interface is the deliberate seam
// for a later persistent history tier (PLAN.md §10); v1 ships only the
// in-memory implementation.
type Store interface {
	// Put adds a room, failing with ErrIDTaken on ID collision or
	// ErrServerFull at the room cap.
	Put(r *Room) error
	// Get looks a room up by ID.
	Get(id string) (*Room, bool)
	// Count reports how many rooms are live.
	Count() int
	// Expire removes and returns every room idle since before cutoff.
	Expire(cutoff time.Time) []*Room
}
