package repository

import (
	"database/sql"

	"cc-053/internal/models"
)

type SpeakerRepo struct {
	db *sql.DB
}

func NewSpeakerRepo(db *sql.DB) *SpeakerRepo {
	return &SpeakerRepo{db: db}
}

func (r *SpeakerRepo) Create(s *models.Speaker) error {
	return r.db.QueryRow(
		`INSERT INTO speakers (code_name, birth_year, gender, dialect_point_code, occupation, years_away, contact_ref, survey_point_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at, updated_at`,
		s.CodeName, s.BirthYear, s.Gender, s.DialectPointCode, s.Occupation, s.YearsAway, s.ContactRef, s.SurveyPointID,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
}

func (r *SpeakerRepo) GetByID(id int64) (*models.Speaker, error) {
	s := &models.Speaker{}
	err := r.db.QueryRow(
		`SELECT id, code_name, birth_year, gender, dialect_point_code, occupation, years_away, contact_ref, survey_point_id, created_at, updated_at
		 FROM speakers WHERE id=$1`, id,
	).Scan(&s.ID, &s.CodeName, &s.BirthYear, &s.Gender, &s.DialectPointCode, &s.Occupation, &s.YearsAway, &s.ContactRef, &s.SurveyPointID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *SpeakerRepo) List(offset, limit int) ([]*models.Speaker, int, error) {
	var total int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM speakers`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := r.db.Query(
		`SELECT id, code_name, birth_year, gender, dialect_point_code, occupation, years_away, contact_ref, survey_point_id, created_at, updated_at
		 FROM speakers ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var speakers []*models.Speaker
	for rows.Next() {
		s := &models.Speaker{}
		if err := rows.Scan(&s.ID, &s.CodeName, &s.BirthYear, &s.Gender, &s.DialectPointCode, &s.Occupation, &s.YearsAway, &s.ContactRef, &s.SurveyPointID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, 0, err
		}
		speakers = append(speakers, s)
	}
	return speakers, total, nil
}