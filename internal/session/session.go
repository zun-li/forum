// Package session enforces one active login per user and bounded JWT renewal.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"forum/pkg/jwt"

	"github.com/redis/go-redis/v9"
)

var ErrReplaced = errors.New("session replaced by another login")

type Options struct {
	AccessTTL     time.Duration
	RenewalWindow time.Duration
	MaxLifetime   time.Duration
}

func (o Options) Validate() error {
	if o.AccessTTL < time.Second || o.AccessTTL > 24*time.Hour || o.AccessTTL%time.Second != 0 {
		return errors.New("session access TTL must be whole seconds between 1s and 24h")
	}
	if o.RenewalWindow < time.Second || o.RenewalWindow >= o.AccessTTL || o.RenewalWindow%time.Second != 0 {
		return errors.New("JWT renewal_window must be whole seconds, positive and shorter than access_ttl")
	}
	if o.MaxLifetime < o.AccessTTL || o.MaxLifetime%time.Second != 0 {
		return errors.New("JWT max_lifetime must be whole seconds and at least access_ttl")
	}
	return nil
}

type Authentication struct {
	UserID             uint64
	SessionID          string
	RenewedToken       string
	MustChangePassword bool
}

type Manager struct {
	tokens  *jwt.Manager
	client  *redis.Client
	options Options
	now     func() time.Time
}

func New(tokens *jwt.Manager, client *redis.Client, options Options) (*Manager, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return &Manager{tokens: tokens, client: client, options: options, now: time.Now}, nil
}

func key(userID uint64) string {
	return "forum:session:" + strconv.FormatUint(userID, 10)
}

var create = redis.NewScript(`
redis.call("HSET", KEYS[1], "sid", ARGV[1], "login_at", ARGV[2],
  "absolute_expiry", ARGV[3], "expires_at", ARGV[4], "must_change_password", ARGV[5])
redis.call("EXPIREAT", KEYS[1], ARGV[4])
return 1
`)

func (m *Manager) Issue(ctx context.Context, userID uint64, mustChangePassword bool) (jwt.AccessToken, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return jwt.AccessToken{}, err
	}
	sid := hex.EncodeToString(random[:])
	now := m.now().UTC().Truncate(time.Second)
	expiresAt := now.Add(m.options.AccessTTL)
	token, err := m.tokens.IssueSession(userID, sid, now, expiresAt)
	if err != nil {
		return jwt.AccessToken{}, err
	}
	err = create.Run(ctx, m.client, []string{key(userID)}, sid, now.Unix(),
		now.Add(m.options.MaxLifetime).Unix(), expiresAt.Unix(), mustChangePassword).Err()
	if err != nil {
		return jwt.AccessToken{}, fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// Validate and extend in one operation. A delayed request can never recreate a
// revoked session or shorten the lifetime established by a concurrent renewal.
var authenticate = redis.NewScript(`
local sid = redis.call("HGET", KEYS[1], "sid")
if not sid then return {0, 0} end
if sid ~= ARGV[1] then return {2, 0} end
local now = tonumber(ARGV[2])
local absolute = tonumber(redis.call("HGET", KEYS[1], "absolute_expiry"))
local expires = tonumber(redis.call("HGET", KEYS[1], "expires_at"))
local token_expiry = tonumber(ARGV[3])
if now >= absolute or now >= expires or token_expiry > expires then return {0, 0} end
local renewed = 0
if token_expiry - now < tonumber(ARGV[4]) then
  local candidate = math.min(now + tonumber(ARGV[5]), absolute)
  if candidate > token_expiry then
    renewed = candidate
    expires = math.max(expires, candidate)
    redis.call("HSET", KEYS[1], "expires_at", expires)
    redis.call("EXPIREAT", KEYS[1], expires)
  end
end
return {1, renewed, tonumber(redis.call("HGET", KEYS[1], "must_change_password") or "0")}
`)

func (m *Manager) Authenticate(ctx context.Context, value string) (Authentication, error) {
	now := m.now().UTC().Truncate(time.Second)
	claims, err := m.tokens.ParseClaims(value, now)
	if err != nil || len(claims.SessionID) != 64 {
		return Authentication{}, jwt.ErrInvalidToken
	}
	userID, _ := strconv.ParseUint(claims.Subject, 10, 63)
	result, err := authenticate.Run(ctx, m.client, []string{key(userID)}, claims.SessionID,
		now.Unix(), claims.ExpiresAt.Unix(), int64(m.options.RenewalWindow/time.Second),
		int64(m.options.AccessTTL/time.Second)).Int64Slice()
	if err != nil {
		return Authentication{}, fmt.Errorf("authenticate session: %w", err)
	}
	if result[0] == 2 {
		return Authentication{}, ErrReplaced
	}
	if result[0] != 1 {
		return Authentication{}, jwt.ErrInvalidToken
	}
	auth := Authentication{UserID: userID, SessionID: claims.SessionID, MustChangePassword: result[2] == 1}
	if result[1] != 0 {
		token, err := m.tokens.IssueSession(userID, claims.SessionID, now, time.Unix(result[1], 0))
		if err != nil {
			return Authentication{}, err
		}
		auth.RenewedToken = token.Value
	}
	return auth, nil
}

var revoke = redis.NewScript(`
if redis.call("HGET", KEYS[1], "sid") == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

func (m *Manager) Revoke(ctx context.Context, userID uint64, sid string) error {
	if err := revoke.Run(ctx, m.client, []string{key(userID)}, sid).Err(); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}
