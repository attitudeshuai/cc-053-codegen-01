package repository

import (
	"database/sql"
	"time"

	"cc-053/internal/models"
)

type RecordingRepo struct {
	db *sql.DB
}

func NewRecordingRepo(db *sql.DB) *RecordingRepo {
	return &RecordingRepo{db: db}
}

func (r *RecordingRepo) Create(rec *models.Recording) error {
	var recordedAt *time.Time
	if rec.RecordedAt != nil {
		recordedAt = rec.RecordedAt
	}
	return r.db.QueryRow(
		`INSERT INTO recordings (task_id, object_key, duration_ms, sample_rate, peak_db, device, recorded_at, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at, updated_at`,
		rec.TaskID, rec.ObjectKey, rec.DurationMs, rec.SampleRate, rec.PeakDB, rec.Device, recordedAt, rec.Status,
	).Scan(&rec.ID, &rec.CreatedAt, &rec.UpdatedAt)
}

func (r *RecordingRepo) GetByID(id int64) (*models.Recording, error) {
	rec := &models.Recording{}
	err := r.db.QueryRow(
		`SELECT id, task_id, object_key, duration_ms, sample_rate, peak_db, device, recorded_at, status, reject_reason, created_at, updated_at
		 FROM recordings WHERE id=$1`, id,
	).Scan(&rec.ID, &rec.TaskID, &rec.ObjectKey, &rec.DurationMs, &rec.SampleRate, &rec.PeakDB, &rec.Device, &rec.RecordedAt, &rec.Status, &rec.RejectReason, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (r *RecordingRepo) UpdateStatus(id int64, status string, rejectReason string) error {
	_, err := r.db.Exec(
		`UPDATE recordings SET status=$1, reject_reason=$2, updated_at=NOW() WHERE id=$3`,
		status, rejectReason, id,
	)
	return err
}

func (r *RecordingRepo) ListByTask(taskID int64) ([]*models.Recording, error) {
	rows, err := r.db.Query(
		`SELECT id, task_id, object_key, duration_ms, sample_rate, peak_db, device, recorded_at, status, reject_reason, created_at, updated_at
		 FROM recordings WHERE task_id=$1 ORDER BY id`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recordings []*models.Recording
	for rows.Next() {
		rec := &models.Recording{}
		if err := rows.Scan(&rec.ID, &rec.TaskID, &rec.ObjectKey, &rec.DurationMs, &rec.SampleRate, &rec.PeakDB, &rec.Device, &rec.RecordedAt, &rec.Status, &rec.RejectReason, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		recordings = append(recordings, rec)
	}
	return recordings, nil
}