package room

import (
	"regexp"
	"testing"
)

func TestWordlist(t *testing.T) {
	if len(words) < 2000 {
		t.Fatalf("wordlist has %d words, want ~2k (PLAN.md §3)", len(words))
	}
	seen := make(map[string]bool, len(words))
	wordRe := regexp.MustCompile(`^[a-z]{3,7}$`)
	for _, w := range words {
		if !wordRe.MatchString(w) {
			t.Errorf("word %q is not 3-7 lowercase ascii letters", w)
		}
		if seen[w] {
			t.Errorf("duplicate word %q", w)
		}
		seen[w] = true
	}
}

func TestRandomIDFormat(t *testing.T) {
	idRe := regexp.MustCompile(`^[a-z]{3,7}-[a-z]{3,7}-\d{2}$`)
	for i := 0; i < 100; i++ {
		id := RandomID()
		if !idRe.MatchString(id) {
			t.Fatalf("RandomID() = %q, want word-word-NN", id)
		}
	}
}

func TestTokens(t *testing.T) {
	raw, hash := newToken()
	if len(raw) != 22 { // 16 bytes base64url, unpadded
		t.Fatalf("token length %d, want 22", len(raw))
	}
	if hashToken(raw) != hash {
		t.Fatal("returned hash does not match token")
	}
	raw2, _ := newToken()
	if raw == raw2 {
		t.Fatal("two tokens identical")
	}
}
