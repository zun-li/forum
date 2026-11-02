package user

import (
	"context"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const recentUsersLimit = 20

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, user *User) error {
	return usernameError(r.db.WithContext(ctx).Create(user).Error)
}

func usernameError(err error) error {
	// 注册与改名共用唯一索引，主键冲突仍作为内部错误返回。
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 &&
		strings.Contains(mysqlError.Message, "idx_users_username") {
		return ErrUsernameTaken
	}
	return err
}

func (r *Repository) List(ctx context.Context) ([]User, error) {
	users := make([]User, 0)
	err := r.db.WithContext(ctx).
		Select("id", "username", "created_at").
		Order("id DESC").
		Limit(recentUsersLimit).
		Find(&users).Error

	return users, err
}

func (r *Repository) FindByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Where("username = ?", username).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) FindByID(ctx context.Context, userID uint64) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).
		Where("id = ?", userID).
		Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) HasAdmin(ctx context.Context) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM users WHERE role = ?)", RoleAdmin).Scan(&exists).Error
	return exists, err
}

func (r *Repository) UpdateUsername(ctx context.Context, userID uint64, username string) error {
	result := r.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Update("username", username)
	if result.Error != nil {
		return usernameError(result.Error)
	}
	if result.RowsAffected == 0 {
		// MySQL may report zero when a concurrent request already saved this name.
		_, err := r.FindByID(ctx, userID)
		return err
	}
	return nil
}

func (r *Repository) UpdatePassword(ctx context.Context, userID uint64, oldHash, newHash string) error {
	result := r.db.WithContext(ctx).Model(&User{}).
		Where("id = ? AND password_hash = ?", userID, oldHash).Update("password_hash", newHash)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrWrongCurrentPassword
	}
	return nil
}

func (r *Repository) SetInitialPassword(ctx context.Context, userID uint64, hash string) error {
	result := r.db.WithContext(ctx).Model(&User{}).
		Where("id = ? AND role = ? AND must_change_password = ?", userID, RoleAdmin, true).
		Updates(map[string]any{"password_hash": hash, "must_change_password": false})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrPasswordAlreadySet
	}
	return nil
}
