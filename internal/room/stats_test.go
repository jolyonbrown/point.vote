package room

import (
	"slices"
	"testing"
)

func votes(values ...string) []RevealedVote {
	vs := make([]RevealedVote, len(values))
	for i, v := range values {
		vs[i] = RevealedVote{Name: "p", Kind: KindHuman, Value: v}
	}
	return vs
}

// statsDeck is a superset deck so every test value has a deck position
// (real rooms enforce membership at vote time).
var statsDeck = []string{"0", "1", "2", "3", "5", "8", "13", "21", "?", "☕", "yes", "no", "NaN", "Inf", "-Inf", "Infinity"}

func TestComputeStats(t *testing.T) {
	tests := []struct {
		name      string
		values    []string
		min, max  float64
		median    float64
		mean      float64
		spread    float64
		consensus bool
		numeric   bool // whether numeric stats should be present
		counts    map[string]int
		topValues []string
		topCount  int
		topTied   bool
	}{
		{
			name: "two spread votes", values: []string{"5", "13"},
			min: 5, max: 13, median: 9, mean: 9, spread: 8, consensus: false, numeric: true,
			counts:    map[string]int{"5": 1, "13": 1},
			topValues: []string{"5", "13"}, topCount: 1, topTied: true,
		},
		{
			name: "consensus", values: []string{"5", "5", "5"},
			min: 5, max: 5, median: 5, mean: 5, spread: 0, consensus: true, numeric: true,
			counts:    map[string]int{"5": 3},
			topValues: []string{"5"}, topCount: 3, topTied: false,
		},
		{
			name: "odd median", values: []string{"1", "8", "13"},
			min: 1, max: 13, median: 8, mean: 22.0 / 3, spread: 12, consensus: false, numeric: true,
			counts:    map[string]int{"1": 1, "8": 1, "13": 1},
			topValues: []string{"1", "8", "13"}, topCount: 1, topTied: true,
		},
		{
			name: "even median", values: []string{"3", "5", "8", "21"},
			min: 3, max: 21, median: 6.5, mean: 9.25, spread: 18, consensus: false, numeric: true,
			counts:    map[string]int{"3": 1, "5": 1, "8": 1, "21": 1},
			topValues: []string{"3", "5", "8", "21"}, topCount: 1, topTied: true,
		},
		{
			name: "clear mode among numerics", values: []string{"5", "5", "13"},
			min: 5, max: 13, median: 5, mean: 23.0 / 3, spread: 8, consensus: false, numeric: true,
			counts:    map[string]int{"5": 2, "13": 1},
			topValues: []string{"5"}, topCount: 2, topTied: false,
		},
		{
			name: "non-numeric excluded from maths", values: []string{"5", "8", "?", "☕"},
			min: 5, max: 8, median: 6.5, mean: 6.5, spread: 3, consensus: false, numeric: true,
			counts:    map[string]int{"5": 1, "8": 1, "?": 1, "☕": 1},
			topValues: []string{"5", "8", "?", "☕"}, topCount: 1, topTied: true,
		},
		{
			name: "single numeric among jokers is consensus", values: []string{"5", "?"},
			min: 5, max: 5, median: 5, mean: 5, spread: 0, consensus: true, numeric: true,
			counts:    map[string]int{"5": 1, "?": 1},
			topValues: []string{"5", "?"}, topCount: 1, topTied: true,
		},
		{
			name: "all non-numeric", values: []string{"yes", "no", "yes"},
			numeric: false, consensus: false,
			counts:    map[string]int{"yes": 2, "no": 1},
			topValues: []string{"yes"}, topCount: 2, topTied: false,
		},
		{
			name: "no votes", values: nil,
			numeric: false, consensus: false,
			counts:    map[string]int{},
			topValues: nil,
		},
		{
			// ParseFloat accepts these literals, but non-finite floats are
			// unmarshalable JSON and would brick the room; they must count
			// as non-numeric.
			name: "non-finite parses excluded", values: []string{"NaN", "Inf", "-Inf", "Infinity"},
			numeric: false, consensus: false,
			counts:    map[string]int{"NaN": 1, "Inf": 1, "-Inf": 1, "Infinity": 1},
			topValues: []string{"NaN", "Inf", "-Inf", "Infinity"}, topCount: 1, topTied: true,
		},
		{
			name: "non-finite alongside numeric", values: []string{"NaN", "5"},
			min: 5, max: 5, median: 5, mean: 5, spread: 0, consensus: true, numeric: true,
			counts:    map[string]int{"NaN": 1, "5": 1},
			topValues: []string{"5", "NaN"}, topCount: 1, topTied: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := computeStats(votes(tt.values...), statsDeck)
			if len(st.Counts) != len(tt.counts) {
				t.Fatalf("counts = %v, want %v", st.Counts, tt.counts)
			}
			for k, n := range tt.counts {
				if st.Counts[k] != n {
					t.Fatalf("counts[%q] = %d, want %d", k, st.Counts[k], n)
				}
			}
			if st.Consensus != tt.consensus {
				t.Fatalf("consensus = %v, want %v", st.Consensus, tt.consensus)
			}

			if tt.topValues == nil {
				if st.Top != nil {
					t.Fatalf("top = %+v, want nil", st.Top)
				}
			} else {
				if st.Top == nil {
					t.Fatal("top = nil, want values")
				}
				if !slices.Equal(st.Top.Values, tt.topValues) || st.Top.Count != tt.topCount || st.Top.Tied != tt.topTied {
					t.Fatalf("top = %+v, want values %v count %d tied %v", st.Top, tt.topValues, tt.topCount, tt.topTied)
				}
			}

			if !tt.numeric {
				if st.Min != nil || st.Max != nil || st.Median != nil || st.Mean != nil || st.Spread != nil {
					t.Fatalf("numeric stats present for non-numeric votes: %+v", st)
				}
				return
			}
			for _, c := range []struct {
				field string
				got   *float64
				want  float64
			}{
				{"min", st.Min, tt.min},
				{"max", st.Max, tt.max},
				{"median", st.Median, tt.median},
				{"mean", st.Mean, tt.mean},
				{"spread", st.Spread, tt.spread},
			} {
				if c.got == nil {
					t.Fatalf("%s = nil, want %v", c.field, c.want)
				}
				if *c.got != c.want {
					t.Fatalf("%s = %v, want %v", c.field, *c.got, c.want)
				}
			}
		})
	}
}
