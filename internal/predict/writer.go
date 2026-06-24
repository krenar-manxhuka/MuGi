package predict

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// WritePredictions writes predictions as JSON Lines — one object per line, the
// shape the official SWE-bench harness reads from a predictions file. Every task
// attempted should appear (an empty model_patch is a valid "unresolved" row), so
// the harness's instance count matches the slice.
func WritePredictions(path string, preds []Prediction) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, p := range preds {
		b, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("marshal prediction %s: %w", p.InstanceID, err)
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}
