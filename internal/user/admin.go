package user

import (
	"context"
	"errors"

	"forum/pkg/password"
)

// SeedAdmin runs before the HTTP listener opens. A previous installation may
// already have an ordinary user named admin; never grant that account access.
func (s *Service) SeedAdmin(ctx context.Context) error {
	admin, err := s.store.FindByUsername(ctx, adminUsername)
	if errors.Is(err, ErrUserNotFound) {
		// Older versions allowed the administrator to change their login name.
		exists, err := s.store.HasAdmin(ctx)
		if err != nil || exists {
			return err
		}
		hash, err := password.Hash(initialPassword)
		if err != nil {
			return err
		}
		err = s.store.Create(ctx, &User{ID: s.ids.Next(), Username: adminUsername,
			Role: RoleAdmin, MustChangePassword: true, PasswordHash: hash})
		if !errors.Is(err, ErrUsernameTaken) {
			return err
		}
		// Another instance may have initialized the same database.
		admin, err = s.store.FindByUsername(ctx, adminUsername)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if admin.Role != RoleAdmin {
		return errors.New("用户名 admin 已被普通账号占用，请先为该账号更名再重启")
	}
	return nil
}

type InitialPasswordInput struct {
	NewPassword  string `json:"new_password" validate:"required,min=6,max=128"`
	Confirmation string `json:"re_password" validate:"required,eqfield=NewPassword"`
}

var errDefaultPassword = errors.New("新密码必须与初始密码不同")

func (s *Service) changeInitialPassword(ctx context.Context, id uint64, input InitialPasswordInput) error {
	if err := s.validate.Struct(input); err != nil {
		return err
	}
	if input.NewPassword == initialPassword {
		return errDefaultPassword
	}
	hash, err := password.Hash(input.NewPassword)
	if err != nil {
		return err
	}
	return s.store.SetInitialPassword(ctx, id, hash)
}
