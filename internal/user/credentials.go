package user

import (
	"context"
	"errors"

	"forum/pkg/jwt"
	"forum/pkg/password"
)

type TokenIssuer interface {
	Issue(ctx context.Context, userID uint64, mustChangePassword bool) (jwt.AccessToken, error)
}

type LoginInput struct {
	Username string `json:"username" validate:"required,min=3,max=32,username"`
	// 登录只检查非空和长度上限，不把注册的最短密码规则套在历史凭证上。
	Password string `json:"password" validate:"required,max=128"`
}

type LoginResult struct {
	User  User
	Token jwt.AccessToken
}

func (s *Service) Login(ctx context.Context, input LoginInput) (*LoginResult, error) {
	input.Username = normalizeUsername(input.Username)
	if err := s.validate.Struct(input); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	user, err := s.store.FindByUsername(ctx, input.Username)
	if errors.Is(err, ErrUserNotFound) {
		// 未注册用户名也执行一次同等成本的哈希，减少明显的时间差。
		if _, hashErr := password.Hash(input.Password); hashErr != nil {
			return nil, hashErr
		}
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}

	matches, err := password.Verify(input.Password, user.PasswordHash)
	if err != nil {
		// 存储的哈希损坏属于内部错误，不能当成普通密码错误。
		return nil, err
	}
	if !matches {
		return nil, ErrInvalidCredentials
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	token, err := s.tokens.Issue(ctx, user.ID, user.MustChangePassword)
	if err != nil {
		return nil, err
	}
	return &LoginResult{User: *user, Token: token}, nil
}

func (s *Service) CurrentUser(ctx context.Context, userID uint64) (*User, error) {
	// JWT 有效不代表用户仍存在，/me 必须查询当前数据库状态。
	return s.store.FindByID(ctx, userID)
}
