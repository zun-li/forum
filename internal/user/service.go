package user

import (
	"context"
	"regexp"
	"strings"

	"forum/pkg/password"

	"github.com/go-playground/validator/v10"
)

// 接口由使用方定义，使业务逻辑不依赖 GORM 或具体 ID 实现。
type Store interface {
	Create(context.Context, *User) error
	List(context.Context) ([]User, error)
	FindByUsername(context.Context, string) (*User, error)
	FindByID(context.Context, uint64) (*User, error)
	HasAdmin(context.Context) (bool, error)
	UpdateUsername(ctx context.Context, userID uint64, username string) error
	UpdatePassword(ctx context.Context, userID uint64, oldHash, newHash string) error
	SetInitialPassword(ctx context.Context, userID uint64, hash string) error
}

type IDGenerator interface {
	Next() uint64
}

type SignupInput struct {
	Username   string `json:"username" validate:"required,min=3,max=32,username"`
	Password   string `json:"password" validate:"required,min=6,max=128"`
	RePassword string `json:"re_password" validate:"required,eqfield=Password"`
}

type Service struct {
	store    Store
	ids      IDGenerator
	validate *validator.Validate
	tokens   TokenIssuer
}

func NewService(store Store, ids IDGenerator, tokens TokenIssuer) *Service {
	validate := validator.New(validator.WithRequiredStructEnabled())
	usernamePattern := regexp.MustCompile(`^[a-z0-9_]+$`)

	err := validate.RegisterValidation("username", func(field validator.FieldLevel) bool {
		return usernamePattern.MatchString(field.Field().String())
	})
	if err != nil {
		// 固定校验规则注册失败属于编程错误，应在初始化阶段暴露。
		panic(err)
	}

	return &Service{
		store:    store,
		tokens:   tokens,
		ids:      ids,
		validate: validate,
	}
}

func normalizeUsername(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (s *Service) Signup(ctx context.Context, input SignupInput) (*User, error) {
	// 用户名规范化后判断唯一性，密码始终保留原样。
	input.Username = normalizeUsername(input.Username)
	if err := s.validate.Struct(input); err != nil {
		return nil, err
	}
	if input.Username == adminUsername {
		return nil, ErrUsernameTaken
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hash, err := password.Hash(input.Password)
	if err != nil {
		return nil, err
	}
	// 哈希计算期间请求可能已取消，此时不再生成 ID 或写入数据库。
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	user := &User{
		ID:           s.ids.Next(),
		Username:     input.Username,
		Role:         RoleUser,
		PasswordHash: hash,
	}
	if err := s.store.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) List(ctx context.Context) ([]User, error) {
	return s.store.List(ctx)
}
