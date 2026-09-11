package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// ---------------------------------------------------------------- passwords (argon2id)

const (
	argonTime    = 3
	argonMemory  = 32 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
)

var b64 = base64.RawStdEncoding

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

func checkPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1
}

// Used for unknown usernames so the response time does not reveal whether a user exists.
var dummyHash = hashPassword("wicket-timing-equalizer")

// ---------------------------------------------------------------- random values

const codeAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func randomString(n int, alphabet string) string {
	max := big.NewInt(int64(len(alphabet)))
	b := make([]byte, n)
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b)
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- TOTP (RFC 6238, SHA1, 6 digits, 30 s)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func newTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

func totpCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", v), nil
}

// verifyTOTP returns the matching time step, or 0. Steps at or before lastStep are
// rejected so a code cannot be replayed.
func verifyTOTP(secret, code string, lastStep int64) int64 {
	code = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, code)
	if len(code) != 6 || secret == "" {
		return 0
	}
	cur := time.Now().Unix() / 30
	for _, s := range []int64{cur, cur - 1, cur + 1} {
		if s <= lastStep {
			continue
		}
		if c, err := totpCode(secret, s); err == nil && subtle.ConstantTimeCompare([]byte(c), []byte(code)) == 1 {
			return s
		}
	}
	return 0
}

func totpURI(secret, user, issuer string) string {
	label := url.PathEscape(issuer + ":" + user)
	return fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30", label, secret, url.QueryEscape(issuer))
}

func groupSecret(s string) string {
	var parts []string
	for i := 0; i < len(s); i += 4 {
		end := i + 4
		if end > len(s) {
			end = len(s)
		}
		parts = append(parts, s[i:end])
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------- recovery codes

func newRecoveryCodes() []string {
	codes := make([]string, 10)
	for i := range codes {
		codes[i] = randomString(4, codeAlphabet) + "-" + randomString(4, codeAlphabet)
	}
	return codes
}

func normalizeCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	if len(s) == 8 && !strings.Contains(s, "-") {
		s = s[:4] + "-" + s[4:]
	}
	return s
}

// ---------------------------------------------------------------- brute-force limiter

type Limiter struct {
	mu    sync.Mutex
	fails map[string][]int64
	until map[string]int64
}

func NewLimiter() *Limiter {
	return &Limiter{fails: map[string][]int64{}, until: map[string]int64{}}
}

// Locked reports whether key is locked and for how many seconds.
func (l *Limiter) Locked(key string) (int64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if u := l.until[key]; u > now() {
		return u - now(), true
	}
	return 0, false
}

// Fail records a failed attempt and returns true when it triggered a lock.
func (l *Limiter) Fail(key string, attempts, windowSec, lockSec int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := now()
	var kept []int64
	for _, f := range l.fails[key] {
		if f > t-int64(windowSec) {
			kept = append(kept, f)
		}
	}
	kept = append(kept, t)
	if len(kept) >= attempts {
		l.until[key] = t + int64(lockSec)
		delete(l.fails, key)
		return true
	}
	l.fails[key] = kept
	return false
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

func (l *Limiter) LockedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, u := range l.until {
		if u > now() {
			n++
		}
	}
	return n
}

func (l *Limiter) Cleanup(windowSec int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := now()
	for k, u := range l.until {
		if u <= t {
			delete(l.until, k)
		}
	}
	for k, fs := range l.fails {
		if len(fs) == 0 || fs[len(fs)-1] < t-int64(windowSec) {
			delete(l.fails, k)
		}
	}
}
