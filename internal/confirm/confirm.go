// Package confirm implements the two-step protocol that gates consequential
// operations.
//
// The first call to a destructive tool performs no write. It returns a
// description of what would change and a confirmation token. Execution requires
// that token on a second call.
//
// The token is not a formality. It is an HMAC over the action name and the
// canonicalised arguments, so it cannot be moved to a different target; it
// expires; and it is consumed on first use, so a retried tool call cannot
// execute the operation twice. A model cannot mint one, because it does not
// have the key.
//
// This deliberately does not rely on the MCP host's tool-approval dialog. That
// dialog is host-dependent, absent in headless deployments, and approves a tool
// rather than a specific target.
package confirm

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/errs"
)

// DefaultTTL is short on purpose. A token should not outlive the exchange that
// justified it.
const DefaultTTL = 5 * time.Minute

// ArgKey is the argument name a caller uses to present a token. It is stripped
// before canonicalisation, so presenting the token does not change the
// fingerprint it was issued against.
const ArgKey = "confirmation_token"

// Token is an issued confirmation.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// Store issues and verifies confirmation tokens.
//
// State is in memory. That means a restart invalidates every outstanding token,
// which is the safe direction, and that this design does not yet support
// running replicas behind a load balancer.
type Store struct {
	key []byte
	ttl time.Duration
	now func() time.Time

	mu       sync.Mutex
	consumed map[string]time.Time
}

// Option configures a Store.
type Option func(*Store)

// WithTTL sets the token lifetime.
func WithTTL(ttl time.Duration) Option {
	return func(s *Store) {
		if ttl > 0 {
			s.ttl = ttl
		}
	}
}

// WithClock injects a clock, for tests. Production always uses time.Now.
func WithClock(now func() time.Time) Option {
	return func(s *Store) {
		if now != nil {
			s.now = now
		}
	}
}

// NewStore creates a store with a fresh random signing key.
//
// The key is generated per process and never persisted, so tokens are not
// transferable between deployments and there is no key to leak from a config
// file.
func NewStore(opts ...Option) (*Store, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate confirmation signing key: %w", err)
	}
	s := &Store{
		key:      key,
		ttl:      DefaultTTL,
		now:      time.Now,
		consumed: make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// TTL returns the configured token lifetime.
func (s *Store) TTL() time.Duration { return s.ttl }

// Issue mints a token bound to an action and its arguments.
func (s *Store) Issue(action string, args map[string]any) (Token, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return Token{}, errs.Wrap(err, errs.CodeInternal, "generate confirmation nonce")
	}
	id := base64.RawURLEncoding.EncodeToString(nonce)
	expires := s.now().Add(s.ttl)

	fp, err := fingerprint(action, args)
	if err != nil {
		return Token{}, err
	}
	mac := s.sign(id, expires.Unix(), fp)

	return Token{
		Value:     strings.Join([]string{"v1", id, strconv.FormatInt(expires.Unix(), 10), mac}, "."),
		ExpiresAt: expires,
	}, nil
}

// Verify checks a token against the action and arguments it is being presented
// for, and consumes it.
//
// Every failure mode is a distinct code, because they mean different things to
// an operator reading an audit log: a mismatch is a token being reused against
// a different target, an expiry is a stale conversation, and a consumed token
// is a duplicate execution that was prevented.
func (s *Store) Verify(token, action string, args map[string]any) error {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return errs.New(errs.CodeConfirmationMismatch,
			"the confirmation token is not a token issued by this server")
	}
	id, expRaw, mac := parts[1], parts[2], parts[3]

	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil {
		return errs.New(errs.CodeConfirmationMismatch, "the confirmation token is malformed")
	}

	fp, err := fingerprint(action, args)
	if err != nil {
		return err
	}

	// Verify the signature before anything else, so an unsigned token cannot
	// probe expiry or consumption state.
	want := s.sign(id, exp, fp)
	if subtle.ConstantTimeCompare([]byte(mac), []byte(want)) != 1 {
		return errs.New(errs.CodeConfirmationMismatch,
			"the confirmation token was not issued for this action and these arguments; request a new preview and confirm that")
	}

	if s.now().After(time.Unix(exp, 0)) {
		return errs.New(errs.CodeConfirmationExpired,
			"the confirmation token expired; request a new preview and confirm that")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if _, used := s.consumed[id]; used {
		return errs.New(errs.CodeConfirmationConsumed,
			"the confirmation token has already been used; the operation it authorised has already run once")
	}
	s.consumed[id] = time.Unix(exp, 0)
	return nil
}

func (s *Store) sign(id string, exp int64, fp string) string {
	m := hmac.New(sha256.New, s.key)
	// Length-prefix each component so that a value containing the separator
	// cannot be shifted between fields.
	for _, part := range []string{id, strconv.FormatInt(exp, 10), fp} {
		// hash.Hash writers never return an error.
		_, _ = fmt.Fprintf(m, "%d:%s|", len(part), part)
	}
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// sweepLocked drops consumed entries that have expired anyway. The signature
// check already rejects an expired token, so retaining it proves nothing.
func (s *Store) sweepLocked() {
	now := s.now()
	for id, exp := range s.consumed {
		if now.After(exp) {
			delete(s.consumed, id)
		}
	}
}

// fingerprint canonicalises an action and its arguments into a stable string.
//
// This is what binds a token to its target. Sorting keys makes the fingerprint
// independent of map iteration order, and removing the token argument means
// presenting the token does not change the value it is checked against.
func fingerprint(action string, args map[string]any) (string, error) {
	clean := make(map[string]any, len(args))
	for k, v := range args {
		if k == ArgKey {
			continue
		}
		clean[strings.ToLower(k)] = v
	}

	keys := make([]string, 0, len(clean))
	for k := range clean {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%d:%s|", len(action), strings.ToLower(action))
	for _, k := range keys {
		// Encode values as JSON so that 42 and "42" are distinguishable: WHMCS
		// treats them alike, but a token should not silently cover both.
		v, err := json.Marshal(clean[k])
		if err != nil {
			return "", errs.Wrap(err, errs.CodeInvalidParams,
				"argument %q cannot be represented in a confirmation token", k)
		}
		_, _ = fmt.Fprintf(h, "%d:%s=%d:%s|", len(k), k, len(v), v)
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}
