package repository

import (
	"database/sql"
	"encoding/json"

	"cc-053/internal/models"
)

type WordlistRepo struct {
	db *sql.DB
}

func NewWordlistRepo(db *sql.DB) *WordlistRepo {
	return &WordlistRepo{db: db}
}

func (r *WordlistRepo) Create(w *models.Wordlist) error {
	entriesJSON, err := json.Marshal(w.Entries)
	if err != nil {
		return err
	}
	return r.db.QueryRow(
		`INSERT INTO wordlists (name, version, entries, is_current)
		 VALUES ($1, 1, $2, TRUE)
		 RETURNING id, version, created_at, updated_at`,
		w.Name, entriesJSON,
	).Scan(&w.ID, &w.Version, &w.CreatedAt, &w.UpdatedAt)
}

func (r *WordlistRepo) GetByID(id int64) (*models.Wordlist, error) {
	w := &models.Wordlist{}
	var entriesJSON []byte
	err := r.db.QueryRow(
		`SELECT id, name, version, entries, is_current, created_at, updated_at
		 FROM wordlists WHERE id=$1`, id,
	).Scan(&w.ID, &w.Name, &w.Version, &entriesJSON, &w.IsCurrent, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(entriesJSON, &w.Entries); err != nil {
		return nil, err
	}
	return w, nil
}

func (r *WordlistRepo) List(offset, limit int) ([]*models.Wordlist, int, error) {
	var total int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM wordlists`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := r.db.Query(
		`SELECT id, name, version, entries, is_current, created_at, updated_at
		 FROM wordlists ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var wordlists []*models.Wordlist
	for rows.Next() {
		w := &models.Wordlist{}
		var entriesJSON []byte
		if err := rows.Scan(&w.ID, &w.Name, &w.Version, &entriesJSON, &w.IsCurrent, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, 0, err
		}
		json.Unmarshal(entriesJSON, &w.Entries)
		wordlists = append(wordlists, w)
	}
	return wordlists, total, nil
}