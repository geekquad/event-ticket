package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type userRepo struct {
	db *sql.DB
}

func NewUserRepo(db *sql.DB) ports.UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) List(ctx context.Context) ([]entities.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, email, created_at FROM users ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []entities.User
	for rows.Next() {
		var u entities.User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user row: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *userRepo) GetByID(ctx context.Context, id string) (*entities.User, error) {
	var u entities.User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, email, created_at FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, entities.ErrNotFound
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}
	return &u, nil
}
