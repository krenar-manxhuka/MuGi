package retrievaleval

import (
	"fmt"
	"sort"
	"strings"
)

// Aggregate summarizes one (mode, k) cell across all instances — the row you read
// to compare retrieval configurations. MeanRecall is the average recall@k over
// instances that evaluated without error; FullHitRate is the fraction where every
// changed file was retrieved (which, for single-file fixes, equals MeanRecall —
// both are reported so multi-file cases stay honest).
type Aggregate struct {
	Mode        string  `json:"mode"`
	K           int     `json:"k"`
	N           int     `json:"n"`      // instances evaluated without error
	Errors      int     `json:"errors"` // instances that failed for this cell
	MeanRecall  float64 `json:"mean_recall_at_k"`
	FullHitRate float64 `json:"full_hit_rate"`
}

// Summarize groups results by (mode, k) and computes the per-cell averages,
// ordered by mode then k for stable, comparable output.
func Summarize(results []InstanceResult) []Aggregate {
	type key struct {
		mode string
		k    int
	}
	cells := map[key]*Aggregate{}
	for _, r := range results {
		ky := key{r.Mode, r.K}
		a := cells[ky]
		if a == nil {
			a = &Aggregate{Mode: r.Mode, K: r.K}
			cells[ky] = a
		}
		if r.Err != "" {
			a.Errors++
			continue
		}
		a.N++
		a.MeanRecall += r.RecallAtK
		if r.FullHit {
			a.FullHitRate++
		}
	}
	out := make([]Aggregate, 0, len(cells))
	for _, a := range cells {
		if a.N > 0 {
			a.MeanRecall /= float64(a.N)
			a.FullHitRate /= float64(a.N)
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Mode != out[j].Mode {
			return out[i].Mode < out[j].Mode
		}
		return out[i].K < out[j].K
	})
	return out
}

// FormatTable renders aggregates as a fixed-width table for the terminal.
func FormatTable(aggs []Aggregate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-10s %4s %4s %8s %12s %12s\n", "mode", "k", "n", "errors", "recall@k", "full-hit")
	fmt.Fprintf(&b, "%-10s %4s %4s %8s %12s %12s\n", "----", "-", "-", "------", "--------", "--------")
	for _, a := range aggs {
		fmt.Fprintf(&b, "%-10s %4d %4d %8d %12.3f %12.3f\n",
			a.Mode, a.K, a.N, a.Errors, a.MeanRecall, a.FullHitRate)
	}
	return b.String()
}
