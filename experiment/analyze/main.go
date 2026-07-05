// Command analyze summarises anchoring-experiment trials: per model and
// arm, the mean deck index; the headline effect (high-anchor mean minus
// low-anchor mean, in deck steps) with a ticket-cluster bootstrap CI; and
// how often anchored estimates moved toward their anchor relative to the
// blind median. Reads trials.jsonl, writes a markdown summary to stdout.
//
// The bootstrap resamples TICKETS, not trials: repetitions of the same
// ticket are not independent observations, and the design is crossed by
// ticket. The trial-level CI is reported alongside for comparison.
//
// -rationales prints every anchored rationale matching the documented
// anchor-reference pattern, so the "N of M rationales mention the anchor"
// claim is reproducible rather than asserted.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"slices"
	"sort"
)

var deck = []string{"0", "1", "2", "3", "5", "8", "13", "21"}

var anchorIdx = map[string]int{"low": 2, "high": 7} // "2" and "21"

// anchorMentionRe is the documented pattern for "the rationale references
// the colleague's vote". Deliberately broad; -rationales prints every
// match so false positives are visible rather than silently counted.
var anchorMentionRe = regexp.MustCompile(`(?i)colleague|other estimator|other panelist|already submitted|shared board|their (vote|estimate)|anchor`)

type trial struct {
	Model     string `json:"model"`
	Ticket    string `json:"ticket"`
	Arm       string `json:"arm"`
	Rep       int    `json:"rep"`
	Value     string `json:"value"`
	Rationale string `json:"rationale"`
	Key       string `json:"key"`
}

func deckIndex(v string) (int, bool) {
	i := slices.Index(deck, v)
	return i, i >= 0
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	ys := slices.Clone(xs)
	sort.Float64s(ys)
	n := len(ys)
	if n%2 == 1 {
		return ys[n/2]
	}
	return (ys[n/2-1] + ys[n/2]) / 2
}

// quantile uses the nearest-rank-on-sorted convention over n-1 intervals.
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	i := int(math.Round(q * float64(len(sorted)-1)))
	return sorted[i]
}

// rng is a tiny deterministic PRNG (xorshift64*) so the analysis is
// reproducible without seeding globals.
type rng struct{ s uint64 }

func newRng(seed uint64) *rng { return &rng{s: seed} }
func (r *rng) next() uint64 {
	r.s ^= r.s >> 12
	r.s ^= r.s << 25
	r.s ^= r.s >> 27
	return r.s * 0x2545F4914F6CDD1D
}
func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }

// clusterBootstrapCI resamples tickets with replacement; for each
// resample, the effect is mean(high trials of sampled tickets) −
// mean(low trials of sampled tickets).
func clusterBootstrapCI(byTicket map[string]map[string][]float64) (lo, hi float64) {
	var tickets []string
	for t, arms := range byTicket {
		if len(arms["low"]) > 0 && len(arms["high"]) > 0 {
			tickets = append(tickets, t)
		}
	}
	sort.Strings(tickets)
	if len(tickets) == 0 {
		return math.NaN(), math.NaN()
	}
	const n = 10000
	r := newRng(42)
	diffs := make([]float64, n)
	for i := range diffs {
		var his, los []float64
		for range tickets {
			t := tickets[r.intn(len(tickets))]
			his = append(his, byTicket[t]["high"]...)
			los = append(los, byTicket[t]["low"]...)
		}
		diffs[i] = mean(his) - mean(los)
	}
	sort.Float64s(diffs)
	return quantile(diffs, 0.025), quantile(diffs, 0.975)
}

// trialBootstrapCI is the naive trial-level resample, reported for
// comparison only.
func trialBootstrapCI(a, b []float64) (lo, hi float64) {
	if len(a) == 0 || len(b) == 0 {
		return math.NaN(), math.NaN()
	}
	const n = 10000
	r := newRng(42)
	resample := func(xs []float64) []float64 {
		out := make([]float64, len(xs))
		for i := range out {
			out[i] = xs[r.intn(len(xs))]
		}
		return out
	}
	diffs := make([]float64, n)
	for i := range diffs {
		diffs[i] = mean(resample(a)) - mean(resample(b))
	}
	sort.Float64s(diffs)
	return quantile(diffs, 0.025), quantile(diffs, 0.975)
}

func main() {
	path := flag.String("in", "experiment/results/trials.jsonl", "trials file")
	rationales := flag.Bool("rationales", false, "print anchored rationales matching the anchor-reference pattern and exit")
	flag.Parse()

	f, err := os.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	indices := map[string]map[string][]float64{}              // model → arm → deck indices
	perTicket := map[string]map[string]map[string][]float64{} // model → ticket → arm → indices
	models := map[string]bool{}
	failures, total := 0, 0
	var anchored []trial

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var t trial
		if err := json.Unmarshal(sc.Bytes(), &t); err != nil {
			log.Fatalf("bad line: %v", err)
		}
		total++
		idx, ok := deckIndex(t.Value)
		if !ok {
			failures++
			continue
		}
		if t.Arm != "blind" {
			anchored = append(anchored, t)
		}
		models[t.Model] = true
		if indices[t.Model] == nil {
			indices[t.Model] = map[string][]float64{}
			perTicket[t.Model] = map[string]map[string][]float64{}
		}
		indices[t.Model][t.Arm] = append(indices[t.Model][t.Arm], float64(idx))
		if perTicket[t.Model][t.Ticket] == nil {
			perTicket[t.Model][t.Ticket] = map[string][]float64{}
		}
		perTicket[t.Model][t.Ticket][t.Arm] = append(perTicket[t.Model][t.Ticket][t.Arm], float64(idx))
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}

	if *rationales {
		matched := 0
		for _, t := range anchored {
			if anchorMentionRe.MatchString(t.Rationale) {
				matched++
				fmt.Printf("MATCH %s: %q\n", t.Key, t.Rationale)
			}
		}
		fmt.Printf("\n%d of %d anchored rationales match %s\n", matched, len(anchored), anchorMentionRe)
		fmt.Println("(inspect matches above for false positives before quoting a count)")
		return
	}

	fmt.Printf("# Anchoring experiment — summary\n\n")
	fmt.Printf("%d trials, %d unusable (no vote / off-deck).\n\n", total, failures)
	fmt.Printf("Deck: %v (analysis in deck-index steps; index 4 = \"5\", 7 = \"21\").\n", deck)
	fmt.Printf("Headline CI is a ticket-cluster bootstrap (tickets resampled, not trials);\nthe trial-level CI is shown for comparison.\n\n")

	names := make([]string, 0, len(models))
	for m := range models {
		names = append(names, m)
	}
	sort.Strings(names)

	for _, m := range names {
		fmt.Printf("## %s\n\n", m)
		fmt.Printf("| arm | n | mean deck idx | mean card |\n|---|---|---|---|\n")
		for _, arm := range []string{"blind", "low", "high"} {
			xs := indices[m][arm]
			card := "-"
			if len(xs) > 0 {
				card = deck[int(math.Round(mean(xs)))]
			}
			fmt.Printf("| %s | %d | %.2f | %s |\n", arm, len(xs), mean(xs), card)
		}
		effect := mean(indices[m]["high"]) - mean(indices[m]["low"])
		clo, chi := clusterBootstrapCI(perTicket[m])
		tlo, thi := trialBootstrapCI(indices[m]["high"], indices[m]["low"])
		fmt.Printf("\n**Anchor effect (high − low): %.2f deck steps** (95%% ticket-cluster CI %.2f to %.2f; trial-level %.2f to %.2f)\n\n",
			effect, clo, chi, tlo, thi)

		// Per-ticket effects: the sign-consistency check.
		fmt.Printf("Per-ticket effects: ")
		var tickets []string
		for t := range perTicket[m] {
			tickets = append(tickets, t)
		}
		sort.Strings(tickets)
		pos, neg := 0, 0
		for _, t := range tickets {
			arms := perTicket[m][t]
			if len(arms["low"]) == 0 || len(arms["high"]) == 0 {
				continue
			}
			e := mean(arms["high"]) - mean(arms["low"])
			if e > 0 {
				pos++
			} else if e < 0 {
				neg++
			}
			fmt.Printf("%s %+.1f  ", t, e)
		}
		fmt.Printf("\n(%d positive, %d negative)\n\n", pos, neg)

		// Direction of drift vs the blind median. The anchor's side of
		// the blind median decides "toward"; ties (anchor == median) are
		// reported separately rather than assigned a direction.
		toward, away, same, undefined := 0, 0, 0, 0
		for _, arms := range perTicket[m] {
			blind := median(arms["blind"])
			if math.IsNaN(blind) {
				continue
			}
			for _, arm := range []string{"low", "high"} {
				dir := float64(anchorIdx[arm]) - blind
				for _, x := range arms[arm] {
					switch {
					case dir == 0:
						undefined++
					case x == blind:
						same++
					case (x-blind)*dir > 0:
						toward++
					default:
						away++
					}
				}
			}
		}
		fmt.Printf("Anchored trials vs blind median: %d moved toward the anchor, %d away, %d unmoved", toward, away, same)
		if undefined > 0 {
			fmt.Printf(", %d direction-undefined (anchor equals the blind median)", undefined)
		}
		fmt.Printf(".\n\n")
	}
}
