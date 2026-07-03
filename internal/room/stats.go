package room

import (
	"sort"
	"strconv"
)

// computeStats counts every value and does the maths over the votes that
// parse as numbers. Consensus means at least one numeric vote and all
// numeric votes equal.
func computeStats(votes []RevealedVote) Stats {
	counts := make(map[string]int, len(votes))
	var nums []float64
	for _, v := range votes {
		counts[v.Value]++
		if f, err := strconv.ParseFloat(v.Value, 64); err == nil {
			nums = append(nums, f)
		}
	}
	st := Stats{Counts: counts}
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
