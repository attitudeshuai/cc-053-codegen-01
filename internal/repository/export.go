package repository

import (
	"database/sql"
	"encoding/json"

	"cc-053/internal/models"
)

type ExportRepo struct {
	db *sql.DB
}

func NewExportRepo(db *sql.DB) *ExportRepo {
	return &ExportRepo{db: db}
}

func (r *ExportRepo) Create(job *models.ExportJob) error {
	var filterJSON json.RawMessage
	if job.Filter != "" {
		filterJSON = json.RawMessage(job.Filter)
	} else {
		filterJSON = json.RawMessage("{}")
	}
	return r.db.QueryRow(
		`INSERT INTO export_jobs (filter, status, progress)
		 VALUES ($1, 'pending', 0)
		 RETURNING id, created_at, updated_at`,
		filterJSON,
	).Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt)
}

func (r *ExportRepo) GetByID(id int64) (*models.ExportJob, error) {
	job := &models.ExportJob{}
	var filterJSON []byte
	err := r.db.QueryRow(
		`SELECT id, filter, status, progress, output_key, error_message, created_at, updated_at
		 FROM export_jobs WHERE id=$1`, id,
	).Scan(&job.ID, &filterJSON, &job.Status, &job.Progress, &job.OutputKey, &job.ErrorMessage, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, err
	}
	job.Filter = string(filterJSON)
	return job, nil
}

func (r *ExportRepo) UpdateProgress(id int64, progress int, status string, outputKey string, errMsg string) error {
	_, err := r.db.Exec(
		`UPDATE export_jobs SET progress=$1, status=$2, output_key=$3, error_message=$4, updated_at=NOW() WHERE id=$5`,
		progress, status, outputKey, errMsg, id,
	)
	return err
}

func (r *ExportRepo) List(offset, limit int) ([]*models.ExportJob, int, error) {
	var total int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM export_jobs`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := r.db.Query(
		`SELECT id, filter, status, progress, output_key, error_message, created_at, updated_at
		 FROM export_jobs ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var jobs []*models.ExportJob
	for rows.Next() {
		job := &models.ExportJob{}
		var filterJSON []byte
		if err := rows.Scan(&job.ID, &filterJSON, &job.Status, &job.Progress, &job.OutputKey, &job.ErrorMessage, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, 0, err
		}
		job.Filter = string(filterJSON)
		jobs = append(jobs, job)
	}
	return jobs, total, nil
}