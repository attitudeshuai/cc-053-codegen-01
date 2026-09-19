package repository

import (
	"database/sql"
	"strings"

	"github.com/lib/pq"

	"cc-053/internal/models"
)

type RegionRepo struct {
	db *sql.DB
}

func NewRegionRepo(db *sql.DB) *RegionRepo {
	return &RegionRepo{db: db}
}

const regionColumns = `id, name, code, level, parent_id, path, note, created_at,
	(SELECT COUNT(*)=0 FROM dialect_regions c WHERE c.parent_id = r.id) AS is_leaf`

func scanRegion(s interface {
	Scan(dest ...any) error
}, g *models.DialectRegion) error {
	var parent sql.NullInt64
	var path pq.Int64Array
	if err := s.Scan(
		&g.ID, &g.Name, &g.Code, &g.Level, &parent, &path, &g.Note, &g.CreatedAt, &g.IsLeaf,
	); err != nil {
		return err
	}
	if parent.Valid {
		pid := parent.Int64
		g.ParentID = &pid
	}
	g.Path = []int64(path)
	return nil
}

// Create 建方言区。有 parent 则层级与路径自动继承（parent.level+1）；
// 无 parent 的是一级区。
func (r *RegionRepo) Create(g *models.DialectRegion) error {
	if g.ParentID != nil {
		parent := &models.DialectRegion{}
		var ppath pq.Int64Array
		err := r.db.QueryRow(
			`SELECT id, name, code, level, parent_id, path, note, created_at FROM dialect_regions WHERE id=$1`,
			*g.ParentID,
		).Scan(&parent.ID, &parent.Name, &parent.Code, &parent.Level, &parent.ParentID, &ppath, &parent.Note, &parent.CreatedAt)
		if err == sql.ErrNoRows {
			return bizErr("not_found", "上级方言区不存在", "")
		}
		if err != nil {
			return err
		}
		g.Level = parent.Level + 1
		path := append([]int64(ppath), parent.ID)
		err = r.db.QueryRow(
			`INSERT INTO dialect_regions (name, code, level, parent_id, path, note)
			 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`,
			g.Name, g.Code, g.Level, g.ParentID, pq.Int64Array(path), g.Note,
		).Scan(&g.ID, &g.CreatedAt)
		if isUnique(err) {
			return bizErr("conflict", "方言区编码已存在", g.Code)
		}
		g.Path = path
		g.IsLeaf = true
		return err
	}

	// 一级区
	g.Level = 1
	err := r.db.QueryRow(
		`INSERT INTO dialect_regions (name, code, level, parent_id, path, note)
		 VALUES ($1,$2,1,NULL,'{}',$3) RETURNING id, created_at`,
		g.Name, g.Code, g.Note,
	).Scan(&g.ID, &g.CreatedAt)
	if isUnique(err) {
		return bizErr("conflict", "方言区编码已存在", g.Code)
	}
	g.Path = []int64{}
	g.IsLeaf = true
	return err
}

func (r *RegionRepo) GetByID(id int64) (*models.DialectRegion, error) {
	g := &models.DialectRegion{}
	q := `SELECT ` + regionColumns + ` FROM dialect_regions r WHERE r.id=$1`
	if err := scanRegion(r.db.QueryRow(q, id), g); err != nil {
		if err == sql.ErrNoRows {
			return nil, bizErr("not_found", "方言区不存在", "")
		}
		return nil, err
	}
	return g, nil
}

func (r *RegionRepo) List(offset, limit int) ([]*models.DialectRegion, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM dialect_regions`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(
		`SELECT `+regionColumns+` FROM dialect_regions r ORDER BY r.path, r.id LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.DialectRegion
	for rows.Next() {
		g := &models.DialectRegion{}
		if err := scanRegion(rows, g); err != nil {
			return nil, 0, err
		}
		out = append(out, g)
	}
	return out, total, nil
}

// All 返回全部方言区（按路径排序），用于拼树
func (r *RegionRepo) All() ([]*models.DialectRegion, error) {
	rows, err := r.db.Query(`SELECT ` + regionColumns + ` FROM dialect_regions r ORDER BY r.path, r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.DialectRegion
	for rows.Next() {
		g := &models.DialectRegion{}
		if err := scanRegion(rows, g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// IsLeaf 该区是否没有下级（只有叶子区能挂调查点）
func (r *RegionRepo) IsLeaf(id int64) (bool, error) {
	var leaf bool
	err := r.db.QueryRow(
		`SELECT NOT EXISTS(SELECT 1 FROM dialect_regions c WHERE c.parent_id=$1)`, id,
	).Scan(&leaf)
	return leaf, err
}

// NamePath 取「一级区/片/小片/本区」的名称全路径，供快照与展示
func (r *RegionRepo) NamePath(id int64) (leafName string, pathText string, err error) {
	var path pq.Int64Array
	if err = r.db.QueryRow(
		`SELECT path FROM dialect_regions WHERE id=$1`, id,
	).Scan(&path); err != nil {
		if err == sql.ErrNoRows {
			return "", "", bizErr("not_found", "方言区不存在", "")
		}
		return "", "", err
	}
	ids := append([]int64(path), id)
	names := make([]string, 0, len(ids))
	for _, rid := range ids {
		var nm string
		if err = r.db.QueryRow(`SELECT name FROM dialect_regions WHERE id=$1`, rid).Scan(&nm); err != nil {
			return "", "", err
		}
		names = append(names, nm)
	}
	return names[len(names)-1], strings.Join(names, "/"), nil
}
