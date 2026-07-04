package room

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// computeAwards hands out the end-of-session confetti when a room settles.
//
// Design rule: accuracy awards anchor to the EARLIEST retained round's
// blind votes measured against the settled value. A blind first vote
// cannot have herded, so "closest" rewards independent judgement — never
// agreement-seeking, which is the failure mode this product exists to
// kill. Distances are deck-index steps, which works for ordinal decks the
// way raw arithmetic does not.
//
// Participants are keyed by name: RoundSummary deliberately carries no
// participant IDs, and rooms are small enough that a duplicate name is a
// social problem, not a statistical one.
func computeAwards(rounds []RoundSummary, deck []string, settled string) []Award {
	if len(rounds) == 0 {
		return []Award{}
	}
	settledIdx := slices.Index(deck, settled)
	if settledIdx < 0 {
		return []Award{} // callers validate; belt and braces
	}
	awards := []Award{}

	// When the settled value is numeric, only numeric first votes compete
	// for the accuracy awards: "?" means "I don't know", and crowning it
	// The Oracle would be nonsense. Non-numeric settles keep whole-deck
	// index distances — decks like t-shirt sizes are ordinal, and if a
	// room settles on "?" it deserves whatever awards it gets.
	settledNumeric := isNumericValue(settled)

	first := rounds[0]
	type firstVote struct {
		name     string
		value    string
		distance int
	}
	var firsts []firstVote
	for _, v := range first.Votes {
		if settledNumeric && !isNumericValue(v.Value) {
			continue
		}
		if idx := slices.Index(deck, v.Value); idx >= 0 {
			firsts = append(firsts, firstVote{v.Name, v.Value, abs(idx - settledIdx)})
		}
	}

	// The Oracle: nearest blind first vote to the settled value.
	if len(firsts) > 0 {
		best := firsts[0].distance
		for _, f := range firsts {
			best = min(best, f.distance)
		}
		var names []string
		for _, f := range firsts {
			if f.distance == best {
				names = append(names, f.name)
			}
		}
		detail := fmt.Sprintf("called %s blind in round %d", settled, first.Seq)
		if best > 0 {
			detail = fmt.Sprintf("blind round-%d vote landed %d card(s) from %s", first.Seq, best, settled)
		}
		awards = append(awards, Award{
			ID: "oracle", Title: "The Oracle", Names: names, Detail: detail,
		})
	}

	// Saw Something: the furthest blind first vote — the outlier who
	// forced the argument. Only exists when there WAS disagreement.
	if len(firsts) > 1 {
		worst := 0
		for _, f := range firsts {
			worst = max(worst, f.distance)
		}
		best := firsts[0].distance
		for _, f := range firsts {
			best = min(best, f.distance)
		}
		if worst > best {
			var names []string
			var values []string
			for _, f := range firsts {
				if f.distance == worst {
					names = append(names, f.name)
					if !slices.Contains(values, f.value) {
						values = append(values, f.value)
					}
				}
			}
			awards = append(awards, Award{
				ID: "saw_something", Title: "Saw Something",
				Names:  names,
				Detail: fmt.Sprintf("voted %s blind and made everyone argue about it", strings.Join(values, "/")),
			})
		}
	}

	// The Diplomat: didn't start on the settled value, finished on it —
	// updated on evidence, which is the Delphi loop working.
	if len(rounds) > 1 {
		last := rounds[len(rounds)-1]
		startedElsewhere := map[string]bool{}
		for _, v := range first.Votes {
			if v.Value != settled {
				startedElsewhere[v.Name] = true
			}
		}
		var names []string
		for _, v := range last.Votes {
			if v.Value == settled && startedElsewhere[v.Name] {
				names = append(names, v.Name)
			}
		}
		if len(names) > 0 {
			awards = append(awards, Award{
				ID: "diplomat", Title: "The Diplomat", Names: names,
				Detail: "read the rationales, moved with grace",
			})
		}
	}

	// The Optimist / The Pessimist: net signed deck-distance across every
	// vote cast, numeric decks only — direction means nothing for
	// yes/no/abstain. Pure banter: this is bias against the group, not
	// against reality, and the copy should never pretend otherwise.
	if settledNumeric {
		net := map[string]int{}
		for _, round := range rounds {
			for _, v := range round.Votes {
				if !isNumericValue(v.Value) {
					continue
				}
				if idx := slices.Index(deck, v.Value); idx >= 0 {
					net[v.Name] += idx - settledIdx
				}
			}
		}
		if len(net) > 1 {
			low, high := 0, 0
			for _, d := range net {
				low = min(low, d)
				high = max(high, d)
			}
			if low < 0 {
				awards = append(awards, Award{
					ID: "optimist", Title: "The Optimist",
					Names:  namesWithNet(net, low),
					Detail: "reliably below the line",
				})
			}
			if high > 0 {
				awards = append(awards, Award{
					ID: "pessimist", Title: "The Pessimist",
					Names:  namesWithNet(net, high),
					Detail: "reliably above it",
				})
			}
		}
	}

	return awards
}

func namesWithNet(net map[string]int, target int) []string {
	var names []string
	for name, d := range net {
		if d == target {
			names = append(names, name)
		}
	}
	slices.Sort(names) // map order is not a personality trait
	return names
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// isNumericValue mirrors the stats maths: finite parses only.
func isNumericValue(v string) bool {
	f, err := strconv.ParseFloat(v, 64)
	return err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}
