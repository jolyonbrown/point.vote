// Command analyze summarises anchoring-experiment trials: per model and
// arm, the mean deck index; the headline effect (high-anchor mean minus
// low-anchor mean, in deck steps); and how often anchored estimates moved
// toward their anchor relative to the blind median. Reads trials.jsonl,
// writes a markdown summary to stdout.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"slices"
	"sort"
)

var deck = []string{"0", "1", "2", "3", "5", "8", "13", "21"}

type trial struct {
	Model  string `json:"model"`
	Ticket string `json:"ticket"`
	Arm    string `json:"arm"`
	Rep    int    `json:"rep"`
	Value  string `json:"value"`
	Room   string `json:"room"`
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

// bootstrapCI returns a 95% CI for the difference of means between two
// samples, by resampling each side. Deterministic seed: this is a summary
// tool, not a casino.
func bootstrapCI(a, b []float64) (lo, hi float64) {
	if len(a) == 0 || len(b) == 0 {
		return math.NaN(), math.NaN()
	}
	const n = 10000
	rng := newRng(42)
	diffs := make([]float64, n)
	for i := range diffs {
		diffs[i] = mean(resample(rng, a)) - mean(resample(rng, b))
	}
	sort.Float64s(diffs)
	return diffs[int(0.025*n)], diffs[int(0.975*n)]
}

func resample(rng *rng, xs []float64) []float64 {
	out := make([]float64, len(xs))
	for i := range out {
		out[i] = xs[rng.intn(len(xs))]
	}
	return out
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

func main() {
	path := flag.String("in", "experiment/results/trials.jsonl", "trials file")
	flag.Parse()

	f, err := os.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// indices[model][arm] = deck indices; perTicket[model][ticket][arm]
	indices := map[string]map[string][]float64{}
	perTicket := map[string]map[string]map[string][]float64{}
	models := map[string]bool{}
	failures := 0
	total := 0

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

	fmt.Printf("# Anchoring experiment — summary\n\n")
	fmt.Printf("%d trials, %d unusable (no vote / off-deck).\n\n", total, failures)
	fmt.Printf("Deck: %v (analysis in deck-index steps; index 4 = \"5\", 7 = \"21\").\n\n", deck)

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
		lo, hi := bootstrapCI(indices[m]["high"], indices[m]["low"])
		effect := mean(indices[m]["high"]) - mean(indices[m]["low"])
		fmt.Printf("\n**Anchor effect (high − low): %.2f deck steps** (95%% CI %.2f to %.2f)\n\n", effect, lo, hi)

		// Direction-of-drift vs the blind median, per ticket.
		toward, away, same := 0, 0, 0
		for _, arms := range perTicket[m] {
			blind := median(arms["blind"])
			if math.IsNaN(blind) {
				continue
			}
			for _, x := range arms["low"] {
				switch {
				case x < blind:
					toward++
				case x > blind:
					away++
				default:
					same++
				}
			}
			for _, x := range arms["high"] {
				switch {
				case x > blind:
					toward++
				case x < blind:
					away++
				default:
					same++
				}
			}
		}
		if toward+away+same > 0 {
			fmt.Printf("Anchored trials vs blind median: %d moved toward the anchor, %d away, %d unmoved.\n\n",
				toward, away, same)
		}
	}
}
