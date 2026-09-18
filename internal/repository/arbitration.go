package repository

import (
	"database/sql"

	"cc-053/internal/models"
)

type ArbitrationRepo struct {
	db *sql.DB
}

func NewArbitrationRepo(db *sql.DB) *ArbitrationRepo {
	return &ArbitrationRepo{db: db}
}

func (r *ArbitrationRepo) Create(a *models.Arbitration) error {
	return r.db.QueryRow(
		`INSERT INTO arbitrations (segment_id, winner_annotation_id, arbiter, reason)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		a.SegmentID, a.WinnerAnnotationID, a.Arbiter, a.Reason,
	).Scan(&a.ID, &a.CreatedAt)
}

func (r *ArbitrationRepo) GetBySegment(segmentID int64) (*models.Arbitration, error) {
	a := &models.Arbitration{}
	err := r.db.QueryRow(
		`SELECT id, segment_id, winner_annotation_id, arbiter, reason, created_at
		 FROM arbitrations WHERE segment_id=$1`, segmentID,
	).Scan(&a.ID, &a.SegmentID, &a.WinnerAnnotationID, &a.Arbiter, &a.Reason, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}