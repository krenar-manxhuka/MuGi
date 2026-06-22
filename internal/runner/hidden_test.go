package runner

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"mugi/internal/models"
)

// loadHiddenTest reads the hidden_test block from a bench task YAML. The runner
// tests run each shipped hidden test against a known-correct reference solution,
// so a hidden test that is itself buggy (a false negative on correct code) fails
// CI before it can mis-score a real model.
func loadHiddenTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var y struct {
		HiddenTest string `yaml:"hidden_test"`
	}
	if err := yaml.Unmarshal(b, &y); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if strings.TrimSpace(y.HiddenTest) == "" {
		t.Fatalf("%s has no hidden_test", path)
	}
	return y.HiddenTest
}

func goMod(module string) models.File {
	return models.File{Path: "go.mod", Lang: "text", Content: "module " + module + "\n\ngo 1.21\n"}
}

func correctFizzbuzz() *models.Artifact {
	return &models.Artifact{Summary: "fizzbuzz", Revision: 1, Files: []models.File{
		goMod("example.com/fizzbuzz"),
		{Path: "fizzbuzz.go", Lang: "go", Content: `package fizzbuzz

import "strconv"

func FizzBuzz(n int) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		switch {
		case i%15 == 0:
			out = append(out, "FizzBuzz")
		case i%3 == 0:
			out = append(out, "Fizz")
		case i%5 == 0:
			out = append(out, "Buzz")
		default:
			out = append(out, strconv.Itoa(i))
		}
	}
	return out
}
`},
	}}
}

func refRevstr() *models.Artifact {
	return &models.Artifact{Summary: "revstr", Revision: 1, Files: []models.File{
		goMod("example.com/revstr"),
		{Path: "revstr.go", Lang: "go", Content: `package revstr

func Reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}
`},
	}}
}

func refFib() *models.Artifact {
	return &models.Artifact{Summary: "fib", Revision: 1, Files: []models.File{
		goMod("example.com/fib"),
		{Path: "fib.go", Lang: "go", Content: `package fib

var memo = map[int]uint64{0: 0, 1: 1}

func Fib(n int) uint64 {
	if n < 0 {
		return 0
	}
	if v, ok := memo[n]; ok {
		return v
	}
	v := Fib(n-1) + Fib(n-2)
	memo[n] = v
	return v
}
`},
	}}
}

func refLRU() *models.Artifact {
	return &models.Artifact{Summary: "lru", Revision: 1, Files: []models.File{
		goMod("example.com/lru"),
		{Path: "lru.go", Lang: "go", Content: `package lru

import "container/list"

type entry[K comparable, V any] struct {
	key K
	val V
}

type Cache[K comparable, V any] struct {
	cap   int
	ll    *list.List
	items map[K]*list.Element
}

func New[K comparable, V any](capacity int) *Cache[K, V] {
	if capacity <= 0 {
		panic("lru: capacity must be > 0")
	}
	return &Cache[K, V]{cap: capacity, ll: list.New(), items: make(map[K]*list.Element)}
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*entry[K, V]).val, true
	}
	var zero V
	return zero, false
}

func (c *Cache[K, V]) Put(key K, value V) {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		el.Value.(*entry[K, V]).val = value
		return
	}
	el := c.ll.PushFront(&entry[K, V]{key, value})
	c.items[key] = el
	if c.ll.Len() > c.cap {
		if oldest := c.ll.Back(); oldest != nil {
			c.ll.Remove(oldest)
			delete(c.items, oldest.Value.(*entry[K, V]).key)
		}
	}
}

func (c *Cache[K, V]) Len() int { return c.ll.Len() }
`},
	}}
}

func refShorten() *models.Artifact {
	return &models.Artifact{Summary: "shorten", Revision: 1, Files: []models.File{
		goMod("example.com/shorten"),
		{Path: "shorten.go", Lang: "go", Content: `package shorten

import (
	"crypto/rand"
	"errors"
	"sync"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const codeLen = 6

type Store struct {
	mu         sync.RWMutex
	longToCode map[string]string
	codeToLong map[string]string
}

func NewStore() *Store {
	return &Store{longToCode: map[string]string{}, codeToLong: map[string]string{}}
}

func (s *Store) Shorten(longURL string) (string, error) {
	if longURL == "" {
		return "", errors.New("shorten: empty URL")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if code, ok := s.longToCode[longURL]; ok {
		return code, nil
	}
	for {
		buf := make([]byte, codeLen)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		code := make([]byte, codeLen)
		for i, b := range buf {
			code[i] = alphabet[int(b)%len(alphabet)]
		}
		cs := string(code)
		if _, clash := s.codeToLong[cs]; clash {
			continue
		}
		s.longToCode[longURL] = cs
		s.codeToLong[cs] = longURL
		return cs, nil
	}
}

func (s *Store) Resolve(code string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.codeToLong[code]
	return v, ok
}
`},
	}}
}

// TestHiddenTests_PassReferenceSolutions proves every shipped hidden test passes
// a known-correct implementation of its task — i.e. the hidden tests have no
// false negatives.
func TestHiddenTests_PassReferenceSolutions(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		art  *models.Artifact
	}{
		{"fizzbuzz", "../../bench/tasks/easy-01-fizzbuzz.yaml", correctFizzbuzz()},
		{"revstr", "../../bench/tasks/easy-02-string-reverse.yaml", refRevstr()},
		{"fib", "../../bench/tasks/easy-03-fibonacci.yaml", refFib()},
		{"lru", "../../bench/tasks/medium-02-lru-cache.yaml", refLRU()},
		{"shorten", "../../bench/tasks/medium-05-url-shortener.yaml", refShorten()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hidden := loadHiddenTest(t, c.yaml)
			res := RunHidden(context.Background(), c.art, hidden, 90*time.Second)
			if !res.BuildOK {
				t.Fatalf("hidden test should build against the reference; output:\n%s", res.BuildOut)
			}
			if !res.TestOK {
				t.Fatalf("hidden test should PASS the reference solution; output:\n%s", res.TestOut)
			}
		})
	}
}

// TestRunHidden_FailsWrongSolution proves the held-out test actually catches a
// wrong implementation (no false positives) using FizzBuzz with broken
// precedence (checks %3 before %15, so 15 -> "Fizz" instead of "FizzBuzz").
func TestRunHidden_FailsWrongSolution(t *testing.T) {
	a := correctFizzbuzz()
	a.Files[1].Content = `package fizzbuzz

import "strconv"

func FizzBuzz(n int) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		switch {
		case i%3 == 0:
			out = append(out, "Fizz")
		case i%5 == 0:
			out = append(out, "Buzz")
		default:
			out = append(out, strconv.Itoa(i))
		}
	}
	return out
}
`
	hidden := loadHiddenTest(t, "../../bench/tasks/easy-01-fizzbuzz.yaml")
	res := RunHidden(context.Background(), a, hidden, 60*time.Second)
	if !res.BuildOK {
		t.Fatalf("wrong solution should still build; output:\n%s", res.BuildOut)
	}
	if res.TestOK {
		t.Fatal("hidden test should FAIL the wrong solution, but it passed")
	}
}

// TestRunHidden_IgnoresModelTests proves the held-out signal is independent of
// the model's own tests: an artifact whose self-test file does not even compile
// must still be scored by the hidden test against its (correct) implementation.
func TestRunHidden_IgnoresModelTests(t *testing.T) {
	a := correctFizzbuzz()
	a.Files = append(a.Files, models.File{
		Path: "fizzbuzz_test.go", Lang: "go",
		Content: "package fizzbuzz\n\nimport \"testing\"\n\nfunc TestModelBroken(t *testing.T) {\n\t_ = thisSymbolDoesNotExist()\n}\n",
	})
	hidden := loadHiddenTest(t, "../../bench/tasks/easy-01-fizzbuzz.yaml")
	res := RunHidden(context.Background(), a, hidden, 60*time.Second)
	if !res.BuildOK {
		t.Fatalf("model's broken test file must be excluded; build output:\n%s", res.BuildOut)
	}
	if !res.TestOK {
		t.Fatalf("hidden test should pass regardless of the model's tests; output:\n%s", res.TestOut)
	}
}
