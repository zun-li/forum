package user

import (
	"context"
	"errors"

	"forum/pkg/password"
)

type UsernameInput struct {
	Username string `json:"username" validate:"required,min=3,max=32,username"`
}

var errAdminRename = errors.New("管理员不能修改名称")

func (s *Service) changeUsername(ctx context.Context, id uint64, input UsernameInput) (string, error) {
	current, err := s.store.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	if current.Role == RoleAdmin {
		return "", errAdminRename
	}
	name := normalizeUsername(input.Username)
	input.Username = name
	if err := s.validate.Struct(input); err != nil {
		return "", err
	}
	if current.Username == name {
		return name, nil
	}
	if name == adminUsername {
		return "", ErrUsernameTaken
	}
	return name, s.store.UpdateUsername(ctx, id, name)
}

var ErrWrongCurrentPassword = errors.New("当前密码不正确")

type PasswordInput struct {
	CurrentPassword string `json:"current_password" validate:"required,max=128"`
	InitialPasswordInput
}

func (s *Service) changePassword(ctx context.Context, id uint64, input PasswordInput) error {
	if err := s.validate.Struct(input); err != nil {
		return err
	}
	current, err := s.store.FindByID(ctx, id)
	if err != nil {
		return err
	}
	matches, err := password.Verify(input.CurrentPassword, current.PasswordHash)
	if err != nil {
		return err
	}
	if !matches {
		return ErrWrongCurrentPassword
	}
	if current.Role == RoleAdmin && input.NewPassword == initialPassword {
		return errDefaultPassword
	}
	hash, err := password.Hash(input.NewPassword)
	if err != nil {
		return err
	}
	return s.store.UpdatePassword(ctx, id, current.PasswordHash, hash)
}
