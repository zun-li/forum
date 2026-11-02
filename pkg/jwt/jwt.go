package jwt

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid access token")

type Options struct {
	Secret   string
	Issuer   string
	Audience string
}

func (o Options) Validate() error {
	if len(o.Secret) < 32 || strings.TrimSpace(o.Secret) == "" {
		return errors.New("JWT secret must contain at least 32 bytes; set JWT_SECRET")
	}
	if strings.TrimSpace(o.Issuer) == "" || strings.TrimSpace(o.Audience) == "" {
		return errors.New("JWT issuer and audience are required")
	}
	return nil
}

// AccessToken 是登录结果，不属于用户数据库模型。
type AccessToken struct {
	Value     string
	ExpiresIn int64
}

type Claims struct {
	SessionID string `json:"sid,omitempty"`
	jwtlib.RegisteredClaims
}

type Manager struct {
	key      []byte
	issuer   string
	audience string
}

func New(options Options) (*Manager, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return &Manager{
		key:      []byte(options.Secret),
		issuer:   options.Issuer,
		audience: options.Audience,
	}, nil
}

func (m *Manager) IssueSession(userID uint64, sessionID string, issuedAt, expiresAt time.Time) (AccessToken, error) {
	// 当前雪花 ID 为正的 63 位整数，sub 使用十进制字符串。
	if userID == 0 || userID > 1<<63-1 {
		return AccessToken{}, errors.New("invalid user ID")
	}
	if !expiresAt.After(issuedAt) {
		return AccessToken{}, errors.New("invalid token lifetime")
	}
	claims := Claims{SessionID: sessionID, RegisteredClaims: jwtlib.RegisteredClaims{
		Subject:   strconv.FormatUint(userID, 10),
		Issuer:    m.issuer,
		Audience:  jwtlib.ClaimStrings{m.audience},
		IssuedAt:  jwtlib.NewNumericDate(issuedAt),
		ExpiresAt: jwtlib.NewNumericDate(expiresAt),
	}}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.key)
	if err != nil {
		return AccessToken{}, fmt.Errorf("sign access token: %w", err)
	}
	return AccessToken{Value: signed, ExpiresIn: int64(expiresAt.Sub(issuedAt) / time.Second)}, nil
}

func (m *Manager) ParseClaims(value string, now time.Time) (*Claims, error) {
	// 限制 Token 长度，防止异常请求制造过大的解析开销。
	if len(value) == 0 || len(value) > 4096 {
		return nil, ErrInvalidToken
	}
	claims := new(Claims)
	token, err := jwtlib.ParseWithClaims(value, claims, func(token *jwtlib.Token) (any, error) {
		return m.key, nil
	},
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithIssuer(m.issuer),
		jwtlib.WithAudience(m.audience),
		jwtlib.WithExpirationRequired(),
		jwtlib.WithIssuedAt(),
		jwtlib.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil || token == nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	// iat 必须存在，且过期时间应晚于签发时间。
	if claims.IssuedAt == nil || claims.ExpiresAt == nil || !claims.ExpiresAt.After(claims.IssuedAt.Time) {
		return nil, ErrInvalidToken
	}
	id, err := strconv.ParseUint(claims.Subject, 10, 63)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != claims.Subject {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
