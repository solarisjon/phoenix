package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/solarisjon/phoenix/internal/model"
)

// SkillRepo persists reusable named skill definitions.
type SkillRepo struct{ db *DB }

func NewSkillRepo(db *DB) *SkillRepo {
	return &SkillRepo{db: db}
}

const skillCols = `id, name, slug, description, instructions, enabled, created_at`

func scanSkill(row interface {
	Scan(...any) error
}) (*model.Skill, error) {
	var sk model.Skill
	var enabled int
	var createdAt string
	if err := row.Scan(&sk.ID, &sk.Name, &sk.Slug, &sk.Description, &sk.Instructions, &enabled, &createdAt); err != nil {
		return nil, err
	}
	sk.Enabled = enabled == 1
	if t, err := time.Parse("2006-01-02 15:04:05", createdAt); err == nil {
		sk.CreatedAt = t
	}
	return &sk, nil
}

func (r *SkillRepo) List(ctx context.Context) ([]*model.Skill, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+skillCols+` FROM skills ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Skill
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (r *SkillRepo) ListEnabled(ctx context.Context) ([]*model.Skill, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+skillCols+` FROM skills WHERE enabled = 1 ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Skill
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (r *SkillRepo) Get(ctx context.Context, id string) (*model.Skill, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+skillCols+` FROM skills WHERE id = ?`, id)
	sk, err := scanSkill(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return sk, err
}

func (r *SkillRepo) GetBySlug(ctx context.Context, slug string) (*model.Skill, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+skillCols+` FROM skills WHERE slug = ?`, slug)
	sk, err := scanSkill(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return sk, err
}

func (r *SkillRepo) Create(ctx context.Context, sk *model.Skill) error {
	enabled := 0
	if sk.Enabled {
		enabled = 1
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO skills (id, name, slug, description, instructions, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sk.ID, sk.Name, sk.Slug, sk.Description, sk.Instructions, enabled,
		sk.CreatedAt.UTC().Format("2006-01-02 15:04:05"))
	return err
}

func (r *SkillRepo) Update(ctx context.Context, sk *model.Skill) error {
	enabled := 0
	if sk.Enabled {
		enabled = 1
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE skills SET name=?, slug=?, description=?, instructions=?, enabled=? WHERE id=?`,
		sk.Name, sk.Slug, sk.Description, sk.Instructions, enabled, sk.ID)
	return err
}

func (r *SkillRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM skills WHERE id = ?`, id)
	return err
}
