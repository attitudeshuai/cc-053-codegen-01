package repository

import (
	"database/sql"

	"cc-053/internal/models"
)

type CountyRepo struct {
	db *sql.DB
}

func NewCountyRepo(db *sql.DB) *CountyRepo {
	return &CountyRepo{db: db}
}

func (r *CountyRepo) Create(c *models.County) error {
	err := r.db.QueryRow(
		`INSERT INTO counties (name, code, note) VALUES ($1,$2,$3)
		 RETURNING id, created_at`,
		c.Name, c.Code, c.Note,
	).Scan(&c.ID, &c.CreatedAt)
	if isUnique(err) {
		return bizErr("conflict", "县编码已存在", c.Code)
	}
	return err
}

func (r *CountyRepo) GetByID(id int64) (*models.County, error) {
	c := &models.County{}
	err := r.db.QueryRow(
		`SELECT id, name, code, note, created_at FROM counties WHERE id=$1`, id,
	).Scan(&c.ID, &c.Name, &c.Code, &c.Note, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, bizErr("not_found", "县不存在", "")
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *CountyRepo) List(offset, limit int) ([]*models.County, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM counties`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(
		`SELECT id, name, code, note, created_at FROM counties
		 ORDER BY id ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.County
	for rows.Next() {
		c := &models.County{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Code, &c.Note, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, nil
}
