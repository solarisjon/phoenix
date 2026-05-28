package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/solarisjon/phoenix/internal/model"
)

type UserRepo struct{ db *DB }

func NewUserRepo(db *DB) *UserRepo { return &UserRepo{db} }

func (r *UserRepo) Get(ctx context.Context, id string) (*model.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, email, settings, created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (r *UserRepo) GetDefault(ctx context.Context) (*model.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, email, settings, created_at FROM users ORDER BY created_at ASC LIMIT 1`)
	return scanUser(row)
}

func (r *UserRepo) Create(ctx context.Context, u *model.User) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, name, email, settings) VALUES (?, ?, ?, ?)`,
		u.ID, u.Name, u.Email, u.Settings)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *UserRepo) Update(ctx context.Context, u *model.User) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET name = ?, email = ?, settings = ? WHERE id = ?`,
		u.Name, u.Email, u.Settings, u.ID)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Settings, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}
