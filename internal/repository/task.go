package repository

import (
	"database/sql"

	"cc-053/internal/models"
)

type TaskRepo struct {
	db *sql.DB
}

func NewTaskRepo(db *sql.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) Create(t *models.Task) error {
	return r.db.QueryRow(
		`INSERT INTO tasks (wordlist_id, speaker_id, kind, assignee, status)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at, updated_at`,
		t.WordlistID, t.SpeakerID, t.Kind, t.Assignee, t.Status,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func (r *TaskRepo) GetByID(id int64) (*models.Task, error) {
	t := &models.Task{}
	err := r.db.QueryRow(
		`SELECT id, wordlist_id, speaker_id, kind, assignee, status, created_at, updated_at
		 FROM tasks WHERE id=$1`, id,
	).Scan(&t.ID, &t.WordlistID, &t.SpeakerID, &t.Kind, &t.Assignee, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *TaskRepo) UpdateStatus(id int64, status string) error {
	_, err := r.db.Exec(
		`UPDATE tasks SET status=$1, updated_at=NOW() WHERE id=$2`, status, id,
	)
	return err
}

func (r *TaskRepo) List(offset, limit int) ([]*models.Task, int, error) {
	var total int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := r.db.Query(
		`SELECT id, wordlist_id, speaker_id, kind, assignee, status, created_at, updated_at
		 FROM tasks ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tasks []*models.Task
	for rows.Next() {
		t := &models.Task{}
		if err := rows.Scan(&t.ID, &t.WordlistID, &t.SpeakerID, &t.Kind, &t.Assignee, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
	}
	return tasks, total, nil
}