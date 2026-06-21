package shorten

import (
	"crypto/rand"
	"errors"
	"sync"
)

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const codeLength = 6

// Store is an in-memory URL shortener store.
// It is safe for concurrent use.
type Store struct {
	mu         sync.RWMutex
	longToCode map[string]string
	codeToLong map[string]string
}

// NewStore creates and returns a new, empty Store.
func NewStore() *Store {
	return &Store{
		longToCode: make(map[string]string),
		codeToLong: make(map[string]string),
	}
}

// Shorten returns a 6-character base62 code for the given long URL.
// Calling Shorten with the same URL multiple times returns the same code.
// Shorten returns a non-nil error if longURL is empty.
func (s *Store) Shorten(longURL string) (string, error) {
	if longURL == "" {
		return "", errors.New("shorten: longURL must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Idempotency: return existing code if the URL has been shortened before.
	if code, exists := s.longToCode[longURL]; exists {
		return code, nil
	}

	// Generate a collision-free 6-character base62 code.
	code, err := s.generateCode()
	if err != nil {
		return "", err
	}

	s.longToCode[longURL] = code
	s.codeToLong[code] = longURL

	return code, nil
}

// generateCode generates a unique 6-character base62 code.
// Must be called with s.mu held for writing.
func (s *Store) generateCode() (string, error) {
	buf := make([]byte, codeLength)
	for {
		if _, err := rand.Read(buf); err != nil {
			return "", errors.New("shorten: failed to generate random code: " + err.Error())
		}

		code := make([]byte, codeLength)
		for i, b := range buf {
			code[i] = base62Alphabet[int(b)%len(base62Alphabet)]
		}

		codeStr := string(code)
		if _, collision := s.codeToLong[codeStr]; !collision {
			return codeStr, nil
		}
		// Collision (extremely rare): retry.
	}
}

// Resolve returns the original long URL associated with the given code.
// It returns the URL and true on a cache hit, or "", false on a miss.
func (s *Store) Resolve(code string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	longURL, ok := s.codeToLong[code]
	return longURL, ok
}
