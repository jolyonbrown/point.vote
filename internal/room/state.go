package room

// Wire types: the JSON room state served by GET /api/v1/rooms/{id}, the SSE
// stream, and the long-poll — one shape for every read path (PLAN.md §4).

// State is the full (redacted while voting) room state.
type State struct {
	RoomID     string         `json:"room_id"`
	Deck       []string       `json:"deck"`
	AutoReveal bool           `json:"auto_reveal"`
	Round      RoundState     `json:"round"`
	Results    *Results       `json:"results"` // null until the round is revealed
	History    []RoundSummary `json:"history"`
}

// RoundState describes the current round without vote contents.
type RoundState struct {
	Seq          int                `json:"seq"`
	Subject      string             `json:"subject"`
	Context      string             `json:"context"`
	State        string             `json:"state"`
	VotesCast    int                `json:"votes_cast"`
	Participants []ParticipantState `json:"participants"`
}

// ParticipantState is the public view of a participant.
type ParticipantState struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	HasVoted bool   `json:"has_voted"`
}

// Results carries revealed votes and their stats.
type Results struct {
	Votes []RevealedVote `json:"votes"`
	Stats Stats          `json:"stats"`
}

// RevealedVote is one participant's revealed vote. Rationale is always
// present (possibly empty) so the §4 votes shape is stable for the web UI
// and openapi.yaml.
type RevealedVote struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	Rationale string `json:"rationale"`
}

// Stats summarises revealed votes. The numeric fields are null when no vote
// parses as a number; non-numeric votes ("?", "☕") are counted but excluded
// from the maths.
type Stats struct {
	Min       *float64       `json:"min"`
	Max       *float64       `json:"max"`
	Median    *float64       `json:"median"`
	Mean      *float64       `json:"mean"`
	Spread    *float64       `json:"spread"`
	Consensus bool           `json:"consensus"`
	Counts    map[string]int `json:"counts"`
}

// RoundSummary is an archived round in the session-only history ring.
type RoundSummary struct {
	Seq     int            `json:"seq"`
	Subject string         `json:"subject"`
	Votes   []RevealedVote `json:"votes"`
	Stats   Stats          `json:"stats"`
}

// Event is a room change notification: joined, left, voted, revealed or
// round_started. State is the full post-change snapshot — snapshots beat
// diffs for correctness and make clients trivial.
type Event struct {
	ID    int
	Name  string
	State State
}
