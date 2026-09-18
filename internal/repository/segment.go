package repository

import (
	"database/sql"
	"fmt"
	"strings"

	"cc-053/internal/models"
)

type SegmentRepo struct {
	db *sql.DB
}

func NewSegmentRepo(db *sql.DB) *SegmentRepo {
	return &SegmentRepo{db: db}
}

func (r *SegmentRepo) Create(s *models.Segment) error {
	return r.db.QueryRow(
		`INSERT INTO segments (recording_id, entry_id, start_ms, end_ms, object_key, snr_db, status, version)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 1)
		 RETURNING id, version, created_at, updated_at`,
		s.RecordingID, s.EntryID, s.StartMs, s.EndMs, s.ObjectKey, s.SnrDB, s.Status,
	).Scan(&s.ID, &s.Version, &s.CreatedAt, &s.UpdatedAt)
}

func (r *SegmentRepo) BulkCreate(segments []*models.Segment) error {
	if len(segments) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO segments (recording_id, entry_id, start_ms, end_ms, object_key, snr_db, status, version)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending', 1)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range segments {
		if _, err := stmt.Exec(s.RecordingID, s.EntryID, s.StartMs, s.EndMs, s.ObjectKey, s.SnrDB); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *SegmentRepo) GetByID(id int64) (*models.Segment, error) {
	s := &models.Segment{}
	err := r.db.QueryRow(
		`SELECT id, recording_id, entry_id, start_ms, end_ms, object_key, snr_db, status, version, created_at, updated_at
		 FROM segments WHERE id=$1`, id,
	).Scan(&s.ID, &s.RecordingID, &s.EntryID, &s.StartMs, &s.EndMs, &s.ObjectKey, &s.SnrDB, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *SegmentRepo) UpdateStatus(id int64, status string, version int) error {
	result, err := r.db.Exec(
		`UPDATE segments SET status=$1, version=version+1, updated_at=NOW() WHERE id=$2 AND version=$3`,
		status, id, version,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("optimistic lock conflict: segment %d version %d", id, version)
	}
	return nil
}

func (r *SegmentRepo) UpdateStatusAndKey(id int64, status string, objectKey string, version int) error {
	result, err := r.db.Exec(
		`UPDATE segments SET status=$1, object_key=$2, version=version+1, updated_at=NOW() WHERE id=$3 AND version=$4`,
		status, objectKey, id, version,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("optimistic lock conflict: segment %d version %d", id, version)
	}
	return nil
}

func (r *SegmentRepo) ClearByRecordingID(recordingID int64) error {
	_, err := r.db.Exec(`DELETE FROM segments WHERE recording_id=$1`, recordingID)
	return err
}

func (r *SegmentRepo) ListByRecordingID(recordingID int64) ([]*models.Segment, error) {
	rows, err := r.db.Query(
		`SELECT id, recording_id, entry_id, start_ms, end_ms, object_key, snr_db, status, version, created_at, updated_at
		 FROM segments WHERE recording_id=$1 ORDER BY entry_id`, recordingID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []*models.Segment
	for rows.Next() {
		s := &models.Segment{}
		if err := rows.Scan(&s.ID, &s.RecordingID, &s.EntryID, &s.StartMs, &s.EndMs, &s.ObjectKey, &s.SnrDB, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		segments = append(segments, s)
	}
	return segments, nil
}

func (r *SegmentRepo) List(query models.SegmentQuery) ([]*models.Segment, int, error) {
	query.Normalize()

	var conditions []string
	var args []interface{}
	argIdx := 1

	if query.TaskID > 0 {
		conditions = append(conditions, fmt.Sprintf("seg.id IN (SELECT s2.id FROM segments s2 JOIN recordings r2 ON r2.id = s2.recording_id WHERE r2.task_id = $%d)", argIdx))
		args = append(args, query.TaskID)
		argIdx++
	}
	if query.Status != "" {
		conditions = append(conditions, fmt.Sprintf("seg.status = $%d", argIdx))
		args = append(args, query.Status)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM segments seg %s", whereClause)
	if err := r.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dataQuery := fmt.Sprintf(
		`SELECT seg.id, seg.recording_id, seg.entry_id, seg.start_ms, seg.end_ms, seg.object_key, seg.snr_db, seg.status, seg.version, seg.created_at, seg.updated_at
		 FROM segments seg %s ORDER BY seg.id LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1,
	)
	args = append(args, query.Limit, query.Offset)

	rows, err := r.db.Query(dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var segments []*models.Segment
	for rows.Next() {
		s := &models.Segment{}
		if err := rows.Scan(&s.ID, &s.RecordingID, &s.EntryID, &s.StartMs, &s.EndMs, &s.ObjectKey, &s.SnrDB, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, 0, err
		}
		segments = append(segments, s)
	}
	return segments, total, nil
}

func (r *SegmentRepo) CountByRecordingID(recordingID int64) (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM segments WHERE recording_id=$1`, recordingID).Scan(&count)
	return count, err
}