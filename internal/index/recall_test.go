package index

import (
	"reflect"
	"testing"
)

func TestChangedFiles(t *testing.T) {
	diff := `diff --git a/src/foo.py b/src/foo.py
--- a/src/foo.py
+++ b/src/foo.py
@@ -1,3 +1,3 @@
-old
+new
diff --git a/pkg/new.py b/pkg/new.py
new file mode 100644
--- /dev/null
+++ b/pkg/new.py
@@ -0,0 +1,2 @@
+added
diff --git a/old.py b/old.py
deleted file mode 100644
--- a/old.py
+++ /dev/null
@@ -1 +0,0 @@
-gone
`
	got := ChangedFiles(diff)
	want := []string{"pkg/new.py", "src/foo.py"} // sorted; deletion excluded
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedFiles = %v, want %v", got, want)
	}
}

func TestRecallAtK(t *testing.T) {
	retrieved := []Chunk{
		{Path: "src/foo.py"}, {Path: "src/foo.py"}, {Path: "unrelated.py"},
	}
	cases := []struct {
		name    string
		changed []string
		want    float64
	}{
		{"full hit", []string{"src/foo.py"}, 1.0},
		{"partial", []string{"src/foo.py", "src/bar.py"}, 0.5},
		{"miss", []string{"src/bar.py"}, 0.0},
		{"no changed files", nil, 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RecallAtK(retrieved, c.changed); got != c.want {
				t.Fatalf("RecallAtK = %v, want %v", got, c.want)
			}
		})
	}
}
