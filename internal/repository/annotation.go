package repository

import (
	"database/sql"
	"fmt"

	"cc-053/internal/models"
)

type AnnotationRepo struct {
	db *sql.DB
}

func NewAnnotationRepo(db *sql.DB) *AnnotationRepo {
	return &AnnotationRepo{db: db}
}

func (r *AnnotationRepo) Create(a *models.Annotation) error {
	return r.db.QueryRow(
		`INSERT INTO annotations (segment_id, annotator, ipa, tone, note, decision, version)
		 VALUES ($1, $2, $3, $4, $5, 'pending', 1)
		 RETURNING id, version, created_at, updated_at`,
		a.SegmentID, a.Annotator, a.IPA, a.Tone, a.Note,
	).Scan(&a.ID, &a.Version, &a.CreatedAt, &a.UpdatedAt)
}

func (r *AnnotationRepo) GetByID(id int64) (*models.Annotation, error) {
	a := &models.Annotation{}
	err := r.db.QueryRow(
		`SELECT id, segment_id, annotator, ipa, tone, note, decision, version, created_at, updated_at
		 FROM annotations WHERE id=$1`, id,
	).Scan(&a.ID, &a.SegmentID, &a.Annotator, &a.IPA, &a.Tone, &a.Note, &a.Decision, &a.Version, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *AnnotationRepo) Update(a *models.Annotation) error {
	// Record history
	r.db.Exec(
		`INSERT INTO annotation_history (segment_id, annotation_id, old_ipa, old_tone, new_ipa, new_tone, changed_by)
		 SELECT $1, $2, ipa, tone, $3, $4, $5 FROM annotations WHERE id=$2`,
		a.SegmentID, a.ID, a.IPA, a.Tone, a.Annotator,
	)

	result, err := r.db.Exec(
		`UPDATE annotations SET ipa=$1, tone=$2, note=$3, version=version+1, updated_at=NOW()
		 WHERE id=$4 AND version=$5`,
		a.IPA, a.Tone, a.Note, a.ID, a.Version,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("optimistic lock conflict: annotation %d version %d", a.ID, a.Version)
	}
	return nil
}

func (r *AnnotationRepo) ListBySegment(segmentID int64) ([]*models.Annotation, error) {
	rows, err := r.db.Query(
		`SELECT id, segment_id, annotator, ipa, tone, note, decision, version, created_at, updated_at
		 FROM annotations WHERE segment_id=$1 ORDER BY created_at`, segmentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var annotations []*models.Annotation
	for rows.Next() {
		a := &models.Annotation{}
		if err := rows.Scan(&a.ID, &a.SegmentID, &a.Annotator, &a.IPA, &a.Tone, &a.Note, &a.Decision, &a.Version, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		annotations = append(annotations, a)
	}
	return annotations, nil
}

func (r *AnnotationRepo) CountBySegment(segmentID int64) (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM annotations WHERE segment_id=$1`, segmentID).Scan(&count)
	return count, err
}

func (r *AnnotationRepo) UpdateDecision(id int64, decision string) error {
	_, err := r.db.Exec(
		`UPDATE annotations SET decision=$1, updated_at=NOW() WHERE id=$2`, decision, id,
	)
	return err
}