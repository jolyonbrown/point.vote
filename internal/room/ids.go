package room

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// words.txt is the embedded ~2k-word safe list for room IDs (PLAN.md §3).
//
//go:embed words.txt
var wordsFile string

var words = strings.Fields(wordsFile)

// RandomID returns a memorable, phone-typeable room ID like "mint-otter-42".
// With 2048 words that is ~4.2e8 combinations; the caller regenerates on
// collision. Room IDs are unlisted capability URLs, so use crypto/rand.
func RandomID() string {
	return fmt.Sprintf("%s-%s-%02d", words[randInt(len(words))], words[randInt(len(words))], randInt(100))
}

func newParticipantID() string {
	var b [4]byte
	mustRead(b[:])
	return hex.EncodeToString(b[:])
}

// newToken mints a 128-bit bearer token, returned once as base64url. Only
// its SHA-256 is ever stored.
func newToken() (raw string, hash [32]byte) {
	var b [16]byte
	mustRead(b[:])
	raw = base64.RawURLEncoding.EncodeToString(b[:])
	return raw, hashToken(raw)
}

func hashToken(token string) [32]byte {
	return sha256.Sum256([]byte(token))
}

func randInt(n int) int {
	var b [8]byte
	mustRead(b[:])
	return int(binary.BigEndian.Uint64(b[:]) % uint64(n))
}

func mustRead(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
}
