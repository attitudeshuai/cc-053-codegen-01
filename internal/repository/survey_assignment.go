package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"cc-053/internal/models"
)

// ---------- 归属时间线 ----------

func scanAssignment(s interface{ Scan(dest ...any) error }, a *models.Assignment) error {
	var end sql.NullTime
	if err := s.Scan(
		&a.ID, &a.PointID, &a.RegionID, &a.EffectiveDate, &end, &a.CreatedBy, &a.Note, &a.CreatedAt,
	); err != nil {
		return err
	}
	if end.Valid {
		t := end.Time
		a.EndDate = &t
	}
	return nil
}

const assignmentCols = `id, point_id, region_id, effective_date, end_date, created_by, note, created_at`

// Assignments 回看一个点的完整归属历史（按生效日倒序）
func (r *SurveyPointRepo) Assignments(pointID int64) ([]*models.Assignment, error) {
	rows, err := r.db.Query(
		`SELECT `+assignmentCols+` FROM point_assignments WHERE point_id=$1
		 ORDER BY effective_date DESC, id DESC`, pointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Assignment
	for rows.Next() {
		a := &models.Assignment{}
		if err = scanAssignment(rows, a); err != nil {
			return nil, err
		}
		name, path, err := r.regions.NamePath(a.RegionID)
		if err != nil {
			return nil, err
		}
		a.RegionName = name
		a.RegionPath = path
		out = append(out, a)
	}
	return out, nil
}

// assignmentAt 取某天生效的归属区：effective_date <= d < end_date
func (r *SurveyPointRepo) assignmentAt(q queryable, pointID int64, d time.Time) (*int64, error) {
	var regionID int64
	err := q.QueryRow(
		`SELECT region_id FROM point_assignments
		 WHERE point_id=$1 AND effective_date <= $2
		   AND (end_date IS NULL OR end_date > $2)
		 ORDER BY effective_date DESC LIMIT 1`,
		pointID, d,
	).Scan(&regionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &regionID, nil
}

type queryable interface {
	QueryRow(query string, args ...any) *sql.Row
}

// AssignmentAt 对外暴露的「按日期回看当时归属」
func (r *SurveyPointRepo) AssignmentAt(pointID int64, d time.Time) (*models.Assignment, error) {
	regionID, err := r.assignmentAt(r.db, pointID, d)
	if err != nil || regionID == nil {
		return nil, err
	}
	name, path, err := r.regions.NamePath(*regionID)
	if err != nil {
		return nil, err
	}
	return &models.Assignment{
		PointID:    pointID,
		RegionID:   *regionID,
		RegionName: name,
		RegionPath: path,
	}, nil
}

// AdjustAssignment 调整归属，写明从哪天起算。
// 新生效日必须晚于现行区间的生效日；旧区间在该日收口，新区间从该日起为现行。
// 同一点任意时刻只挂一个叶子区，由排他锁 + 唯一索引共同保证。
func (r *SurveyPointRepo) AdjustAssignment(pointID, regionID int64, effDate time.Time, createdBy, note string) (*models.Assignment, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var status string
	if err = tx.QueryRow(
		`SELECT status FROM survey_points WHERE id=$1 FOR UPDATE`, pointID,
	).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return nil, bizErr("not_found", "调查点不存在", fmt.Sprintf("point_id=%d", pointID))
		}
		return nil, err
	}
	if status == "merged" {
		return nil, bizErr("merged", "该调查点已被合并，不能再调整归属", "")
	}

	if err = requireLeaf(tx, regionID); err != nil {
		return nil, err
	}

	// 现行区间
	var curID int64
	var curEff time.Time
	err = tx.QueryRow(
		`SELECT id, effective_date FROM point_assignments
		 WHERE point_id=$1 AND end_date IS NULL`, pointID,
	).Scan(&curID, &curEff)
	switch {
	case err == nil:
		// 已有现行区间：生效日必须严格晚于它（同日即挂重，当场指出）
		if !effDate.After(curEff) {
			return nil, bizErr("overlap",
				"该调查点此日期已有归属，不能重复挂接",
				fmt.Sprintf("现行归属自 %s 起生效；调整日 %s 不晚于它", curEff.Format("2006-01-02"), effDate.Format("2006-01-02")))
		}
		if _, err = tx.Exec(
			`UPDATE point_assignments SET end_date=$2 WHERE id=$1 AND end_date IS NULL`,
			curID, effDate); err != nil {
			return nil, err
		}
	case err == sql.ErrNoRows:
		// 没有现行区间：可能是首次挂接，也可能在历史空档补录
		var lastEnd sql.NullTime
		var lastEff time.Time
		err = tx.QueryRow(
			`SELECT effective_date, end_date FROM point_assignments
			 WHERE point_id=$1 ORDER BY effective_date DESC LIMIT 1`, pointID,
		).Scan(&lastEff, &lastEnd)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil {
			// 只能接在最后一段之后；落进历史区间内部即重叠
			if lastEnd.Valid && effDate.Before(lastEnd.Time) {
				return nil, bizErr("overlap",
					"新生效日落在此前归属区间内，造成时间线重叠",
					fmt.Sprintf("上一段覆盖至 %s", lastEnd.Time.Format("2006-01-02")))
			}
		}
	default:
		return nil, err
	}

	var a models.Assignment
	var end sql.NullTime
	err = tx.QueryRow(
		`INSERT INTO point_assignments (point_id, region_id, effective_date, end_date, created_by, note)
		 VALUES ($1,$2,$3,NULL,$4,$5)
		 RETURNING `+assignmentCols,
		pointID, regionID, effDate, createdBy, note,
	).Scan(&a.ID, &a.PointID, &a.RegionID, &a.EffectiveDate, &end, &a.CreatedBy, &a.Note, &a.CreatedAt)
	if err != nil {
		if isUnique(err, "ux_assignments_eff") {
			return nil, bizErr("overlap", "该生效日已存在归属区间，不能重复挂接", "")
		}
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE survey_points SET updated_at=NOW() WHERE id=$1`, pointID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	name, path, err := r.regions.NamePath(regionID)
	if err != nil {
		return nil, err
	}
	a.RegionName = name
	a.RegionPath = path
	return &a, nil
}

// ---------- 已发出的说法（归属快照） ----------

// CreateRecord 登记一条说法，按 record_date 当时的归属固化快照；
// 当天该点没有任何归属区间则拒绝。快照之后不受归属调整影响。
func (r *SurveyPointRepo) CreateRecord(pointID int64, content string, recordDate time.Time, createdBy string) (*models.PointRecord, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var countyID int64
	var status string
	if err = tx.QueryRow(
		`SELECT county_id, status FROM survey_points WHERE id=$1 FOR UPDATE`, pointID,
	).Scan(&countyID, &status); err != nil {
		if err == sql.ErrNoRows {
			return nil, bizErr("not_found", "调查点不存在", fmt.Sprintf("point_id=%d", pointID))
		}
		return nil, err
	}
	if status == "merged" {
		return nil, bizErr("merged", "该调查点已被合并，不能再登记说法", "")
	}

	regionID, err := r.assignmentAt(tx, pointID, recordDate)
	if err != nil {
		return nil, err
	}
	if regionID == nil {
		return nil, bizErr("no_assignment",
			fmt.Sprintf("该调查点在 %s 没有归属方言区，无法按当时归属登记", recordDate.Format("2006-01-02")),
			"请先补录该日期的归属区间")
	}

	var countyName string
	if err = tx.QueryRow(`SELECT name FROM counties WHERE id=$1`, countyID).Scan(&countyName); err != nil {
		return nil, err
	}
	leafName, pathText, err := regionNamePathTx(tx, *regionID)
	if err != nil {
		return nil, err
	}

	rec := &models.PointRecord{}
	err = tx.QueryRow(
		`INSERT INTO point_records
			(point_id, content, record_date, snapshot_county, snapshot_region, snapshot_path, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING id, created_at`,
		pointID, content, recordDate, countyName, leafName, pathText, createdBy,
	).Scan(&rec.ID, &rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	rec.PointID = pointID
	rec.Content = content
	rec.RecordDate = recordDate
	rec.SnapshotCounty = countyName
	rec.SnapshotRegion = leafName
	rec.SnapshotPath = pathText
	rec.CreatedBy = createdBy
	return rec, nil
}

func regionNamePathTx(tx *sql.Tx, regionID int64) (string, string, error) {
	var pqPath pq.Int64Array
	if err := tx.QueryRow(`SELECT path FROM dialect_regions WHERE id=$1`, regionID).Scan(&pqPath); err != nil {
		return "", "", err
	}
	path := []int64(pqPath)
	ids := append(path, regionID)
	names := make([]string, 0, len(ids))
	for _, rid := range ids {
		var nm string
		if err := tx.QueryRow(`SELECT name FROM dialect_regions WHERE id=$1`, rid).Scan(&nm); err != nil {
			return "", "", err
		}
		names = append(names, nm)
	}
	return names[len(names)-1], strings.Join(names, "/"), nil
}

// Records 列出一个点的说法（倒序），快照即当时归属、不随后续调整变化
func (r *SurveyPointRepo) Records(pointID int64) ([]*models.PointRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, point_id, content, record_date, snapshot_county, snapshot_region, snapshot_path, created_by, created_at
		 FROM point_records WHERE point_id=$1 ORDER BY record_date DESC, id DESC`, pointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecords(rows)
}

// RecordsAsOf 按「说法成立日的当时归属」回看：返回的每条仍是不可变快照。
// 若需要按某个观察日筛现行点，用 List 的 region/county 过滤。
func (r *SurveyPointRepo) RecordsAsOf(pointID int64, _ time.Time) ([]*models.PointRecord, error) {
	return r.Records(pointID)
}

func scanRecords(rows *sql.Rows) ([]*models.PointRecord, error) {
	var out []*models.PointRecord
	for rows.Next() {
		rec := &models.PointRecord{}
		if err := rows.Scan(
			&rec.ID, &rec.PointID, &rec.Content, &rec.RecordDate,
			&rec.SnapshotCounty, &rec.SnapshotRegion, &rec.SnapshotPath,
			&rec.CreatedBy, &rec.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (r *SurveyPointRepo) countRecordsTx(tx *sql.Tx, pointID int64) (int, error) {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM point_records WHERE point_id=$1`, pointID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// History 一个点的档案 + 归属时间线 + 全部说法
func (r *SurveyPointRepo) History(pointID int64) (*models.PointHistoryResponse, error) {
	p, err := r.GetByID(pointID)
	if err != nil {
		return nil, err
	}
	asg, err := r.Assignments(pointID)
	if err != nil {
		return nil, err
	}
	recs, err := r.Records(pointID)
	if err != nil {
		return nil, err
	}
	return &models.PointHistoryResponse{Point: p, Assignments: asg, Records: recs}, nil
}

// ---------- 合并 ----------

// Merge 把「同一个地方两种名字」的两个档案合成一个：
// surviving 保留、merged 标记作废；说法全部迁到 surviving，两边条数当场核对。
func (r *SurveyPointRepo) Merge(survivingID, mergedID int64, mergedBy, note string) (*models.PointMerge, error) {
	if survivingID == mergedID {
		return nil, bizErr("bad_input", "不能合并同一个调查点", "")
	}
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 锁住两点，串行化合并
	rows, err := tx.Query(
		`SELECT id, status FROM survey_points WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`,
		survivingID, mergedID)
	if err != nil {
		return nil, err
	}
	status := map[int64]string{}
	for rows.Next() {
		var id int64
		var st string
		if err = rows.Scan(&id, &st); err != nil {
			rows.Close()
			return nil, err
		}
		status[id] = st
	}
	rows.Close()
	if len(status) != 2 {
		return nil, bizErr("not_found", "待合并的调查点不存在", "")
	}
	if status[survivingID] != "active" || status[mergedID] != "active" {
		return nil, bizErr("merged", "只能合并两个在用的调查点（已合并的不能再合并）", "")
	}

	survBefore, err := r.countRecordsTx(tx, survivingID)
	if err != nil {
		return nil, err
	}
	mergedBefore, err := r.countRecordsTx(tx, mergedID)
	if err != nil {
		return nil, err
	}

	// 1) 先删 merged 与 surviving 同指纹的别名（本就是同一名字的不同写法）
	if _, err = tx.Exec(
		`DELETE FROM survey_point_aliases WHERE point_id=$1
		 AND fingerprint IN (SELECT fingerprint FROM survey_point_aliases WHERE point_id=$2)`,
		mergedID, survivingID); err != nil {
		return nil, err
	}
	// 2) 其余别名归到 surviving
	if _, err = tx.Exec(
		`UPDATE survey_point_aliases SET point_id=$2, source='merge' WHERE point_id=$1`,
		mergedID, survivingID); err != nil {
		return nil, err
	}

	// 3) 说法迁移，并核对迁移条数
	res, err := tx.Exec(`UPDATE point_records SET point_id=$2 WHERE point_id=$1`, mergedID, survivingID)
	if err != nil {
		return nil, err
	}
	moved, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if int(moved) != mergedBefore {
		return nil, fmt.Errorf("合并条数核对失败：预期迁移 %d 条，实际 %d 条", mergedBefore, moved)
	}

	survAfter, err := r.countRecordsTx(tx, survivingID)
	if err != nil {
		return nil, err
	}
	mergedAfter, err := r.countRecordsTx(tx, mergedID)
	if err != nil {
		return nil, err
	}
	if survAfter != survBefore+mergedBefore || mergedAfter != 0 {
		return nil, fmt.Errorf("合并条数核对失败：合并后 surviving=%d（应 %d），merged=%d（应 0）",
			survAfter, survBefore+mergedBefore, mergedAfter)
	}

	// 4) merged 作废，指向 surviving（其归属时间线保留在原档上备查）
	if _, err = tx.Exec(
		`UPDATE survey_points SET status='merged', merged_into_id=$2, updated_at=NOW() WHERE id=$1`,
		mergedID, survivingID); err != nil {
		return nil, err
	}

	m := &models.PointMerge{
		SurvivingPointID: survivingID,
		MergedPointID:    mergedID,
		SurvivingBefore:  survBefore,
		MergedBefore:     mergedBefore,
		TotalAfter:       survAfter,
		MergedBy:         mergedBy,
		Note:             note,
	}
	if err = tx.QueryRow(
		`INSERT INTO point_merges
			(surviving_point_id, merged_point_id, surviving_before, merged_before, total_after, merged_by, note)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`,
		m.SurvivingPointID, m.MergedPointID, m.SurvivingBefore, m.MergedBefore,
		m.TotalAfter, m.MergedBy, m.Note,
	).Scan(&m.ID, &m.CreatedAt); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return m, nil
}

// MergeHistory 合并审计列表
func (r *SurveyPointRepo) MergeHistory(limit int) ([]*models.PointMerge, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.Query(
		`SELECT id, surviving_point_id, merged_point_id, surviving_before, merged_before,
		        total_after, merged_by, note, created_at
		 FROM point_merges ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.PointMerge
	for rows.Next() {
		m := &models.PointMerge{}
		if err = rows.Scan(
			&m.ID, &m.SurvivingPointID, &m.MergedPointID, &m.SurvivingBefore, &m.MergedBefore,
			&m.TotalAfter, &m.MergedBy, &m.Note, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
