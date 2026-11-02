package user

import (
	"errors"
	"time"
)

// ErrUsernameTaken 是业务错误，不包含数据库实现细节。
var (
	ErrUsernameTaken      = errors.New("username already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrPasswordAlreadySet = errors.New("初始密码已修改，请使用新密码登录")
)

const (
	RoleUser        = "user"
	RoleAdmin       = "admin"
	adminUsername   = "admin"
	initialPassword = "666666"
)

type User struct {
	// ID 由雪花算法生成，数据库不参与自增。
	ID                 uint64 `gorm:"primaryKey;autoIncrement:false"`
	Username           string `gorm:"size:64;not null;uniqueIndex"`
	PasswordHash       string `gorm:"size:255;not null" json:"-"`
	Role               string `gorm:"size:16;not null;default:user"`
	MustChangePassword bool   `gorm:"not null;default:false"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
