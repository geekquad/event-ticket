package application

import (
	"context"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type userService struct {
	userRepo ports.UserRepository
}

func NewUserService(userRepo ports.UserRepository) ports.UserService {
	return &userService{userRepo: userRepo}
}

func (s *userService) ListUsers(ctx context.Context) ([]entities.User, error) {
	return s.userRepo.List(ctx)
}
