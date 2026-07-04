package room

import (
	"math"
	"sort"
	"strconv"
)

// computeStats counts every value and does the maths over the votes that
// parse as numbers. Consensus means at least one numeric vote and all
// numeric votes equal. Non-finite parses ("NaN", "Inf") count as
// non-numeric: they would poison the stats and encoding/json cannot
// marshal them, which would brick every read path of the room.
//
// Top is the modal value(s) — the only "result" a non-numeric decision
// deck has, and the Delphi-diagnostic companion to the median for
// numeric ones. Ties are reported honestly, in deck order.
func computeStats(votes []RevealedVote, deck []string) Stats {
	counts := make(map[string]int, len(votes))
	var nums []float64
	for _, v := range votes {
		counts[v.Value]++
		if f, err := strconv.ParseFloat(v.Value, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			nums = append(nums, f)
		}
	}
	st := Stats{Counts: counts, Top: topOf(counts, deck)}
	if len(nums) == 0 {
		return st
	}
	sort.Float64s(nums)

	sum := 0.0
	for _, f := range nums {
		sum += f
	}
	n := len(nums)
	var median float64
	if n%2 == 1 {
		median = nums[n/2]
	} else {
		median = (nums[n/2-1] + nums[n/2]) / 2
	}

	st.Min = ptr(nums[0])
	st.Max = ptr(nums[n-1])
	st.Median = ptr(median)
	st.Mean = ptr(sum / float64(n))
	st.Spread = ptr(nums[n-1] - nums[0])
	st.Consensus = nums[0] == nums[n-1]
	return st
}

func ptr(f float64) *float64 { return &f }

// topOf finds the modal value(s), ordered by deck position so ties are
// deterministic. Nil when there are no votes.
func topOf(counts map[string]int, deck []string) *Top {
	if len(counts) == 0 {
		return nil
	}
	most := 0
	for _, n := range counts {
		most = max(most, n)
	}
	var values []string
	for _, option := range deck {
		if counts[option] == most {
			values = append(values, option)
		}
	}
	return &Top{Values: values, Count: most, Tied: len(values) > 1}
}
