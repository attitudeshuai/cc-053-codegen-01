package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"cc-053/internal/models"
)

// 档案类业务错误，handler 据此映射 400/404/409
var (
	ErrNotFound          = errors.New("archive: not found")
	ErrAlreadyMerged     = errors.New("archive: survey point has already been merged into another point")
	ErrDoubleAssignment  = errors.New("archive: overlapping assignment: point is already attached to a region for that date")
	ErrRegionNotLeaf     = errors.New("archive: a survey point can only be attached to a leaf (finest-level) region")
	ErrLevelGap          = errors.New("archive: region level must be exactly one below its parent")
	ErrCycle             = errors.New("archive: region hierarchy cycle detected")
	ErrSamePoint         = errors.New("archive: kept_point_id and merged_point_id must differ")
	ErrCountyMismatch    = errors.New("archive: points in different counties cannot be auto-merged; verify they are the same place first")
	ErrNameConflict      = errors.New("archive: a survey point with the same name or alias already exists")
	ErrAliasNameConflict = errors.New("archive: alias name already used by another point")
)

// EarliestDate 自有档案以来的起始日（半开区间下界）
var EarliestDate = time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// County
// ---------------------------------------------------------------------------

type CountyRepo struct {
	db *sql.DB
}

func NewCountyRepo(db *sql.DB) *CountyRepo { return &CountyRepo{db: db} }

func (r *CountyRepo) Create(c *models.County) error {
	return r.db.QueryRow(
		`INSERT INTO counties (name, code, province) VALUES ($1, $2, $3)
		 RETURNING id, created_at, updated_at`,
		c.Name, c.Code, c.Province,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

func (r *CountyRepo) GetByID(id int64) (*models.County, error) {
	c := &models.County{}
	err := r.db.QueryRow(
		`SELECT id, name, code, province, created_at, updated_at FROM counties WHERE id=$1`, id,
	).Scan(&c.ID, &c.Name, &c.Code, &c.Province, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (r *CountyRepo) List(offset, limit int) ([]*models.County, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM counties`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(
		`SELECT id, name, code, province, created_at, updated_at
		 FROM counties ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.County
	for rows.Next() {
		c := &models.County{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Code, &c.Province, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, nil
}

// ---------------------------------------------------------------------------
// DialectRegion
// ---------------------------------------------------------------------------

type RegionRepo struct {
	db *sql.DB
}

func NewRegionRepo(db *sql.DB) *RegionRepo { return &RegionRepo{db: db} }

// Create 建分区。parent 为空即顶级（level 必须为 1）；否则层级必须紧邻 parent.level+1
func (r *RegionRepo) Create(g *models.DialectRegion) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if g.ParentID != nil {
		var parentLevel int
		err := tx.QueryRow(`SELECT level FROM dialect_regions WHERE id=$1 FOR UPDATE`, *g.ParentID).Scan(&parentLevel)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if g.Level != parentLevel+1 {
			return fmt.Errorf("%w: parent level %d, child level %d", ErrLevelGap, parentLevel, g.Level)
		}
	} else if g.Level != 1 {
		return fmt.Errorf("%w: top-level region must have level 1, got %d", ErrLevelGap, g.Level)
	}

	err = tx.QueryRow(
		`INSERT INTO dialect_regions (name, level, parent_id) VALUES ($1, $2, $3)
		 RETURNING id, created_at, updated_at`,
		g.Name, g.Level, g.ParentID,
	).Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *RegionRepo) GetByID(id int64) (*models.DialectRegion, error) {
	g := &models.DialectRegion{}
	err := r.db.QueryRow(
		`SELECT id, name, level, parent_id, created_at, updated_at FROM dialect_regions WHERE id=$1`, id,
	).Scan(&g.ID, &g.Name, &g.Level, &g.ParentID, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

// IsLeaf 该分区下是否再无下级（调查点只能挂叶子）
func (r *RegionRepo) IsLeaf(id int64) (bool, error) {
	var child int64
	err := r.db.QueryRow(`SELECT id FROM dialect_regions WHERE parent_id=$1 LIMIT 1`, id).Scan(&child)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// PathOf 从叶子到根的分区链路（按 level 升序返回）
func (r *RegionRepo) PathOf(id int64) ([]models.RegionBrief, error) {
	rows, err := r.db.Query(`
		WITH RECURSIVE chain AS (
			SELECT id, name, level, parent_id FROM dialect_regions WHERE id=$1
			UNION ALL
			SELECT d.id, d.name, d.level, d.parent_id FROM dialect_regions d
			JOIN chain c ON d.id = c.parent_id
		)
		SELECT id, name, level FROM chain ORDER BY level`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var path []models.RegionBrief
	for rows.Next() {
		b := models.RegionBrief{}
		if err := rows.Scan(&b.ID, &b.Name, &b.Level); err != nil {
			return nil, err
		}
		path = append(path, b)
	}
	// 层级断链意味着数据环（理论上被插入校验挡住），显式报出
	if len(path) > 1 {
		for i := 1; i < len(path); i++ {
			if path[i].Level != path[i-1].Level+1 {
				return nil, ErrCycle
			}
		}
	}
	return path, nil
}

func (r *RegionRepo) List() ([]*models.DialectRegion, error) {
	rows, err := r.db.Query(
		`SELECT id, name, level, parent_id, created_at, updated_at
		 FROM dialect_regions ORDER BY level, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.DialectRegion
	for rows.Next() {
		g := &models.DialectRegion{}
		if err := rows.Scan(&g.ID, &g.Name, &g.Level, &g.ParentID, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// SurveyPoint
// ---------------------------------------------------------------------------

type SurveyPointRepo struct {
	db *sql.DB
}

func NewSurveyPointRepo(db *sql.DB) *SurveyPointRepo { return &SurveyPointRepo{db: db} }

const pointColumns = `id, name, code, county_id, current_region_id, merged_into, note, created_at, updated_at`

func scanPoint(row interface {
	Scan(dest ...interface{}) error
}) (*models.SurveyPoint, error) {
	p := &models.SurveyPoint{}
	if err := row.Scan(&p.ID, &p.Name, &p.Code, &p.CountyID, &p.CurrentRegionID,
		&p.MergedInto, &p.Note, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// Create 建档；同名/同别名当场拒绝（ErrNameConflict，携带已存在档案信息由 service 转译）
func (r *SurveyPointRepo) Create(p *models.SurveyPoint, regionID int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 县必须存在
	var countyExists int
	if err := tx.QueryRow(`SELECT 1 FROM counties WHERE id=$1`, p.CountyID).Scan(&countyExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if err := tx.QueryRow(
		`INSERT INTO survey_points (name, code, county_id, note) VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at, updated_at`,
		p.Name, p.Code, p.CountyID, p.Note,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return err
	}

	if err := insertAliases(tx, p.ID, p.Aliases); err != nil {
		return err
	}

	if regionID != 0 {
		leaf, err := regionLeafInTx(tx, regionID)
		if err != nil {
			return err
		}
		if !leaf {
			return ErrRegionNotLeaf
		}
		// 建点即挂上初始归属，自档案纪元起生效
		if _, err := tx.Exec(
			`INSERT INTO point_assignment_history (point_id, region_id, valid_from)
			 VALUES ($1, $2, $3)`,
			p.ID, regionID, EarliestDate); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`UPDATE survey_points SET current_region_id=$1 WHERE id=$2`, regionID, p.ID); err != nil {
			return err
		}
		p.CurrentRegionID = &regionID
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return r.loadAliases(p)
}

// FindConflict 按点名/别名精确匹配现存档案（不含已合并的墓碑），用于“认重”
func (r *SurveyPointRepo) FindConflict(name string, countyID int64) (*models.SurveyPoint, error) {
	name = strings.TrimSpace(name)
	row := r.db.QueryRow(`
		SELECT sp.id, sp.name, sp.code, sp.county_id, sp.current_region_id,
		       sp.merged_into, sp.note, sp.created_at, sp.updated_at
		FROM survey_points sp
		WHERE sp.merged_into IS NULL
		  AND (
		    (sp.name = $1 AND ($2 = 0 OR sp.county_id = $2))
		    OR EXISTS (SELECT 1 FROM survey_point_aliases a
		              WHERE a.point_id = sp.id AND a.name = $1)
		  )
		LIMIT 1`, name, countyID)
	p, err := scanPoint(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, r.loadAliases(p)
}

// Resolve 沿 merged_into 链归到当前有效档案 id；返回是否发生过跳转
func (r *SurveyPointRepo) Resolve(id int64) (int64, bool, error) {
	canonical := id
	moved := false
	for i := 0; i < 16; i++ {
		var merged sql.NullInt64
		err := r.db.QueryRow(`SELECT merged_into FROM survey_points WHERE id=$1`, canonical).Scan(&merged)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, ErrNotFound
		}
		if err != nil {
			return 0, false, err
		}
		if !merged.Valid {
			return canonical, moved, nil
		}
		canonical = merged.Int64
		moved = true
	}
	return 0, false, ErrCycle
}

func (r *SurveyPointRepo) GetByID(id int64) (*models.SurveyPoint, error) {
	p, err := scanPoint(r.db.QueryRow(
		`SELECT `+pointColumns+` FROM survey_points WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := r.loadAliases(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *SurveyPointRepo) Detail(id int64) (*models.SurveyPointDetail, error) {
	p, err := r.GetByID(id)
	if err != nil {
		return nil, err
	}
	d := &models.SurveyPointDetail{SurveyPoint: *p}
	_ = r.db.QueryRow(`SELECT name, code FROM counties WHERE id=$1`, p.CountyID).Scan(&d.CountyName, &d.CountyCode)
	if p.CurrentRegionID != nil {
		path, err := r.PathOf(*p.CurrentRegionID)
		if err != nil {
			return nil, err
		}
		d.RegionPath = path
	}
	return d, nil
}

// PathOf 透传分区链路，供 handler 拼详情
func (r *SurveyPointRepo) PathOf(regionID int64) ([]models.RegionBrief, error) {
	return NewRegionRepo(r.db).PathOf(regionID)
}

type PointFilter struct {
	CountyID   int64
	RegionID   int64 // 按分区链上任意一级过滤
	Q          string
	OnlyLive   bool // 排除已合并墓碑
	Pagination models.Pagination
}

func (r *SurveyPointRepo) List(f PointFilter) ([]*models.SurveyPointDetail, int, error) {
	var conds []string
	var args []interface{}
	if f.OnlyLive {
		conds = append(conds, "sp.merged_into IS NULL")
	}
	if f.CountyID != 0 {
		args = append(args, f.CountyID)
		conds = append(conds, fmt.Sprintf("sp.county_id=$%d", len(args)))
	}
	if f.Q != "" {
		args = append(args, "%"+strings.TrimSpace(f.Q)+"%")
		conds = append(conds, fmt.Sprintf(
			"(sp.name ILIKE $%d OR sp.code ILIKE $%d OR EXISTS (SELECT 1 FROM survey_point_aliases a WHERE a.point_id=sp.id AND a.name ILIKE $%d))",
			len(args), len(args), len(args)))
	}
	if f.RegionID != 0 {
		args = append(args, f.RegionID)
		// 命中当前归属叶子本身，或其任一祖先分区
		conds = append(conds, fmt.Sprintf(`EXISTS (
			WITH RECURSIVE chain AS (
				SELECT id, parent_id FROM dialect_regions WHERE id = sp.current_region_id
				UNION ALL
				SELECT d.id, d.parent_id FROM dialect_regions d JOIN chain c ON d.id = c.parent_id
			)
			SELECT 1 FROM chain WHERE chain.id = $%d)`, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	countArgs := args
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM survey_points sp `+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, f.Pagination.Limit, f.Pagination.Offset)
	rows, err := r.db.Query(fmt.Sprintf(`
		SELECT %s, c.name, c.code
		FROM survey_points sp
		JOIN counties c ON c.id = sp.county_id
		%s
		ORDER BY sp.id DESC LIMIT $%d OFFSET $%d`,
		qualify(pointColumns, "sp"), where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*models.SurveyPointDetail
	for rows.Next() {
		p := &models.SurveyPoint{}
		d := &models.SurveyPointDetail{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.CountyID, &p.CurrentRegionID,
			&p.MergedInto, &p.Note, &p.CreatedAt, &p.UpdatedAt,
			&d.CountyName, &d.CountyCode); err != nil {
			return nil, 0, err
		}
		d.SurveyPoint = *p
		out = append(out, d)
	}
	return out, total, nil
}

// ---- 归属时间线 ----

// AssignRegion 从 validFrom 起把点挂到新分区。
// 若该日已被既有区间覆盖（挂重了），返回 ErrDoubleAssignment，不改任何数据。
func (r *SurveyPointRepo) AssignRegion(pointID, regionID int64, validFrom time.Time, changedBy, reason string) (*models.Assignment, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var merged sql.NullInt64
	err = tx.QueryRow(`SELECT merged_into FROM survey_points WHERE id=$1 FOR UPDATE`, pointID).Scan(&merged)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if merged.Valid {
		return nil, fmt.Errorf("%w: point %d merged into %d", ErrAlreadyMerged, pointID, merged.Int64)
	}

	leaf, err := regionLeafInTx(tx, regionID)
	if err != nil {
		return nil, err
	}
	if !leaf {
		return nil, ErrRegionNotLeaf
	}

	// 挂重检测（当场指出）。时间线由建档时的初始归属起连续维护，只看“至今行”：
	//  - 新生效日必须严格晚于当前归属的生效日（同日重复挂、往回改都当场拒绝）；
	//  - 目标分区与当前分区相同也算挂重。
	// 正常调整：旧区间恰好闭到 d（valid_to=d），新区间 [d, NULL)，历史行原样保留。
	var openID, openRegion int64
	var openFrom time.Time
	err = tx.QueryRow(`
		SELECT id, region_id, valid_from FROM point_assignment_history
		WHERE point_id=$1 AND valid_to IS NULL ORDER BY valid_from DESC LIMIT 1`,
		pointID).Scan(&openID, &openRegion, &openFrom)
	switch {
	case err == nil:
		if regionID == openRegion {
			return nil, fmt.Errorf("%w: point %d is already attached to region %d",
				ErrDoubleAssignment, pointID, openRegion)
		}
		if !validFrom.After(openFrom) {
			return nil, fmt.Errorf("%w: current assignment id=%d region_id=%d has been effective since %s; new valid_from must be later (issued historical statements are not rewritten)",
				ErrDoubleAssignment, openID, openRegion, openFrom.Format("2006-01-02"))
		}
	case errors.Is(err, sql.ErrNoRows):
		// 首次归属，允许任意生效日
	default:
		return nil, err
	}

	if _, err := tx.Exec(
		`UPDATE point_assignment_history SET valid_to=$1
		 WHERE point_id=$2 AND valid_to IS NULL`, validFrom, pointID); err != nil {
		return nil, err
	}

	a := &models.Assignment{PointID: pointID, RegionID: regionID, ValidFrom: validFrom, ChangedBy: changedBy, Reason: reason}
	if err := tx.QueryRow(
		`INSERT INTO point_assignment_history (point_id, region_id, valid_from, changed_by, reason)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id, created_at`,
		pointID, regionID, validFrom, changedBy, reason).Scan(&a.ID, &a.CreatedAt); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`UPDATE survey_points SET current_region_id=$1, updated_at=NOW() WHERE id=$2`,
		regionID, pointID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

// AssignmentAt 按 as-of 时点回看归属（历史已发出结论不随后续调整改变）。
// 若 id 是已合并的旧档案，自动归到 canonical 档案；优先读旧档案自己的历史行，
// 旧档案当日无行（如合并之后）时才回落到保留点。
func (r *SurveyPointRepo) AssignmentAt(id int64, at time.Time) (*models.AssignmentAt, error) {
	canonical, moved, err := r.Resolve(id)
	if err != nil {
		return nil, err
	}
	group := []int64{canonical}
	if moved {
		group = append(group, id)
	}

	p, err := r.GetByID(canonical)
	if err != nil {
		return nil, err
	}
	res := &models.AssignmentAt{PointID: canonical, PointName: p.Name, At: at}

	// 优先使用“被查询 id 自己”的区间：查旧档案 id 时，其名下历史行原样保留，
	// 已发出的说法不因合并而变；旧档案当日无记录时才回落到保留点。
	row := r.db.QueryRow(`
		SELECT region_id FROM point_assignment_history
		WHERE point_id = ANY($1) AND valid_from <= $2 AND (valid_to IS NULL OR valid_to > $2)
		ORDER BY CASE point_id WHEN $3 THEN 0 ELSE 1 END, valid_from DESC
		LIMIT 1`, pq.Array(group), at, id)
	var regionID int64
	if err := row.Scan(&regionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return res, nil // 该日期尚无归属
		}
		return nil, err
	}
	g, err := NewRegionRepo(r.db).GetByID(regionID)
	if err != nil {
		return nil, err
	}
	res.RegionID = &g.ID
	res.RegionName = g.Name
	res.RegionPath, err = NewRegionRepo(r.db).PathOf(regionID)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (r *SurveyPointRepo) ListAssignments(pointID int64) ([]*models.AssignmentView, error) {
	if _, _, err := r.Resolve(pointID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(`
		SELECT h.id, h.point_id, h.region_id, h.valid_from, h.valid_to, h.changed_by, h.reason, h.created_at, d.name
		FROM point_assignment_history h
		JOIN dialect_regions d ON d.id = h.region_id
		WHERE h.point_id=$1
		ORDER BY h.valid_from DESC`, pointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.AssignmentView
	for rows.Next() {
		a := &models.AssignmentView{}
		if err := rows.Scan(&a.ID, &a.PointID, &a.RegionID, &a.ValidFrom, &a.ValidTo,
			&a.ChangedBy, &a.Reason, &a.CreatedAt, &a.RegionName); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// ---- 异名同地合并 ----

func (r *SurveyPointRepo) MergePoints(keptID, mergedID int64) (*models.MergeResult, error) {
	if keptID == mergedID {
		return nil, ErrSamePoint
	}

	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 固定加锁顺序防死锁
	lo, hi := keptID, mergedID
	if lo > hi {
		lo, hi = hi, lo
	}
	var loMerged, hiMerged sql.NullInt64
	var loCounty, hiCounty int64
	var loName, hiName string
	if err := tx.QueryRow(
		`SELECT merged_into, county_id, name FROM survey_points WHERE id=$1 FOR UPDATE`, lo).
		Scan(&loMerged, &loCounty, &loName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := tx.QueryRow(
		`SELECT merged_into, county_id, name FROM survey_points WHERE id=$1 FOR UPDATE`, hi).
		Scan(&hiMerged, &hiCounty, &hiName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if loMerged.Valid || hiMerged.Valid {
		return nil, ErrAlreadyMerged
	}
	if loCounty != hiCounty {
		return nil, fmt.Errorf("%w: county_id %d vs %d", ErrCountyMismatch, loCounty, hiCounty)
	}

	keptName, mergedName := loName, hiName
	if keptID == hi {
		keptName, mergedName = hiName, loName
	}

	// 合并前两边条数
	before, err := pointCountsInTx(tx, keptID, mergedID)
	if err != nil {
		return nil, err
	}

	// 1) 被并点名作为别名保留；其别名搬到保留点，重名跳过（不丢条数，计入对账）
	aliasNames := []string{mergedName}
	rows, err := tx.Query(`SELECT name FROM survey_point_aliases WHERE point_id=$1`, mergedID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		aliasNames = append(aliasNames, n)
	}
	rows.Close()

	var movedAliases []string
	dupAliases := int64(0)
	for _, n := range aliasNames {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		res, insErr := tx.Exec(
			`INSERT INTO survey_point_aliases (point_id, name) VALUES ($1,$2)
			 ON CONFLICT (point_id, name) DO NOTHING`, keptID, n)
		if insErr != nil {
			return nil, insErr
		}
		if aff, _ := res.RowsAffected(); aff > 0 {
			movedAliases = append(movedAliases, n)
		} else {
			dupAliases++
		}
	}
	if _, err := tx.Exec(`DELETE FROM survey_point_aliases WHERE point_id=$1`, mergedID); err != nil {
		return nil, err
	}

	// 2) 归属时间线：把被并点的区间复制到保留点；与保留点既有区间重叠的跳过
	// （同一日期两边各有说法时以保留点为准——已发出结论仍可在旧档案时间线里原样查到）
	hRows, err := tx.Query(`
		SELECT region_id, valid_from, valid_to, changed_by, reason
		FROM point_assignment_history WHERE point_id=$1 ORDER BY valid_from`, mergedID)
	if err != nil {
		return nil, err
	}
	type hist struct {
		regionID   int64
		from       time.Time
		to         sql.NullTime
		by, reason string
	}
	var mergedHist []hist
	for hRows.Next() {
		var h hist
		if err := hRows.Scan(&h.regionID, &h.from, &h.to, &h.by, &h.reason); err != nil {
			hRows.Close()
			return nil, err
		}
		mergedHist = append(mergedHist, h)
	}
	hRows.Close()

	movedAssignments := int64(0)
	skippedAssignments := int64(0)
	for _, h := range mergedHist {
		// 区间相交谓词 [a,b)∩[c,d)≠∅：a<d AND c<b；NULL 端按 +∞ 处理。
		// 恰好首尾相接（b=c）不算冲突。
		var conflict int64
		err := tx.QueryRow(`
			SELECT 1 FROM point_assignment_history
			WHERE point_id=$1
			  AND valid_from < COALESCE($3, 'infinity'::timestamptz)
			  AND $2 < COALESCE(valid_to, 'infinity'::timestamptz)
			LIMIT 1`,
			keptID, h.from, h.to).Scan(&conflict)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if _, err := tx.Exec(`
				INSERT INTO point_assignment_history (point_id, region_id, valid_from, valid_to, changed_by, reason)
				VALUES ($1,$2,$3,$4,$5,$6)`,
				keptID, h.regionID, h.from, h.to, h.by, h.reason); err != nil {
				return nil, err
			}
			movedAssignments++
		} else {
			skippedAssignments++
		}
	}

	// 3) 发音人（及其下游 task/recording/segment/annotation 条数）整体转到保留点
	res, err := tx.Exec(`UPDATE speakers SET survey_point_id=$1 WHERE survey_point_id=$2`, keptID, mergedID)
	if err != nil {
		return nil, err
	}
	speakersMoved, _ := res.RowsAffected()

	// 3.5) 旧档案“至今”归属行在合并时刻封口：合并日之前的回看仍读旧行，
	// 合并日之后回落到保留点的归属，避免旧点永远停在旧分区
	if _, err := tx.Exec(
		`UPDATE point_assignment_history SET valid_to=$1
		 WHERE point_id=$2 AND valid_to IS NULL`, time.Now().UTC(), mergedID); err != nil {
		return nil, err
	}

	// 4) 被并点立墓碑：清当前归属缓存，指向保留点
	if _, err := tx.Exec(
		`UPDATE survey_points SET merged_into=$1, current_region_id=NULL, updated_at=NOW() WHERE id=$2`,
		keptID, mergedID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	after, err := r.canonicalCounts(keptID)
	if err != nil {
		return nil, err
	}

	// 对账：
	//  - 别名：迁过去的（含被并点本名）= 被并点别名数 + 1，重名跳过的单独计数，不丢不重；
	//  - 归属行：迁走 + 重叠跳过 = 被并点原行数（旧档案的行原样保留，回看旧点仍走旧行）；
	//  - 发音人及其下游语料：合并后合计必须等于两边之和。
	aliasInputs := before.Merged.Aliases + 1 // 被并点本名 + 其别名
	total := func(a, b models.MergePointCounts) models.MergePointCounts {
		return models.MergePointCounts{
			SurveyPoints: a.SurveyPoints + b.SurveyPoints,
			Aliases:      a.Aliases + b.Aliases,
			Speakers:     a.Speakers + b.Speakers,
			Tasks:        a.Tasks + b.Tasks,
			Recordings:   a.Recordings + b.Recordings,
			Segments:     a.Segments + b.Segments,
			Annotations:  a.Annotations + b.Annotations,
			Assignments:  a.Assignments + b.Assignments,
		}
	}
	balanced := speakersMoved == before.Merged.Speakers &&
		int64(len(movedAliases))+dupAliases == aliasInputs &&
		movedAssignments+skippedAssignments == before.Merged.Assignments &&
		after.SurveyPoints == before.Kept.SurveyPoints+before.Merged.SurveyPoints &&
		after.Aliases == before.Kept.Aliases+int64(len(movedAliases)) &&
		after.Speakers == before.Kept.Speakers+before.Merged.Speakers &&
		after.Tasks == before.Kept.Tasks+before.Merged.Tasks &&
		after.Recordings == before.Kept.Recordings+before.Merged.Recordings &&
		after.Segments == before.Kept.Segments+before.Merged.Segments &&
		after.Annotations == before.Kept.Annotations+before.Merged.Annotations &&
		after.Assignments == before.Kept.Assignments+movedAssignments

	note := fmt.Sprintf(
		"aliases moved=%d duplicated=%d; assignments moved=%d overlap-skipped=%d (kept as-is on retired point); speakers moved=%d",
		len(movedAliases), dupAliases, movedAssignments, skippedAssignments, speakersMoved)

	return &models.MergeResult{
		KeptPointID:        keptID,
		MergedPointID:      mergedID,
		KeptName:           keptName,
		KeptCountsBefore:   before.Kept,
		MergedCountsBefore: before.Merged,
		CountsBefore:       total(before.Kept, before.Merged),
		CountsAfter:        after,
		AliasesMoved:       movedAliases,
		AssignmentRows:     movedAssignments,
		Balanced:           balanced,
		ConsistencyNote:    note,
	}, nil
}

type countsPair struct {
	Kept, Merged models.MergePointCounts
}

func pointCountsInTx(tx *sql.Tx, keptID, mergedID int64) (*countsPair, error) {
	c := &countsPair{}
	var err error
	c.Kept, err = pointCountsQuery(tx.QueryRow, keptID)
	if err != nil {
		return nil, err
	}
	c.Merged, err = pointCountsQuery(tx.QueryRow, mergedID)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func pointCountsQuery(q func(string, ...interface{}) *sql.Row, id int64) (models.MergePointCounts, error) {
	var c models.MergePointCounts
	err := q(`
		SELECT
			(SELECT count(*) FROM survey_points WHERE id=$1),
			(SELECT count(*) FROM survey_point_aliases WHERE point_id=$1),
			(SELECT count(*) FROM speakers WHERE survey_point_id=$1),
			(SELECT count(*) FROM tasks t JOIN speakers s ON s.id=t.speaker_id WHERE s.survey_point_id=$1),
			(SELECT count(*) FROM recordings r JOIN tasks t ON t.id=r.task_id JOIN speakers s ON s.id=t.speaker_id WHERE s.survey_point_id=$1),
			(SELECT count(*) FROM segments g JOIN recordings r ON r.id=g.recording_id JOIN tasks t ON t.id=r.task_id JOIN speakers s ON s.id=t.speaker_id WHERE s.survey_point_id=$1),
			(SELECT count(*) FROM annotations a JOIN segments g ON g.id=a.segment_id JOIN recordings r ON r.id=g.recording_id JOIN tasks t ON t.id=r.task_id JOIN speakers s ON s.id=t.speaker_id WHERE s.survey_point_id=$1),
			(SELECT count(*) FROM point_assignment_history WHERE point_id=$1)
	`, id).Scan(&c.SurveyPoints, &c.Aliases, &c.Speakers, &c.Tasks, &c.Recordings,
		&c.Segments, &c.Annotations, &c.Assignments)
	return c, err
}

// canonicalCounts 合并后按保留点（含其名下被并墓碑）统算条数
func (r *SurveyPointRepo) canonicalCounts(keptID int64) (models.MergePointCounts, error) {
	var c models.MergePointCounts
	err := r.db.QueryRow(`
		WITH pts AS (SELECT id FROM survey_points WHERE id=$1 OR merged_into=$1)
		SELECT
			(SELECT count(*) FROM pts),
			(SELECT count(*) FROM survey_point_aliases a JOIN pts p ON p.id=a.point_id),
			(SELECT count(*) FROM speakers s JOIN pts p ON p.id=s.survey_point_id),
			(SELECT count(*) FROM tasks t JOIN speakers s ON s.id=t.speaker_id JOIN pts p ON p.id=s.survey_point_id),
			(SELECT count(*) FROM recordings r JOIN tasks t ON t.id=r.task_id JOIN speakers s ON s.id=t.speaker_id JOIN pts p ON p.id=s.survey_point_id),
			(SELECT count(*) FROM segments g JOIN recordings r ON r.id=g.recording_id JOIN tasks t ON t.id=r.task_id JOIN speakers s ON s.id=t.speaker_id JOIN pts p ON p.id=s.survey_point_id),
			(SELECT count(*) FROM annotations a JOIN segments g ON g.id=a.segment_id JOIN recordings r ON r.id=g.recording_id JOIN tasks t ON t.id=r.task_id JOIN speakers s ON s.id=t.speaker_id JOIN pts p ON p.id=s.survey_point_id),
			(SELECT count(*) FROM point_assignment_history WHERE point_id=$1)
	`, keptID).Scan(&c.SurveyPoints, &c.Aliases, &c.Speakers, &c.Tasks, &c.Recordings,
		&c.Segments, &c.Annotations, &c.Assignments)
	return c, err
}

// Counts 供 API 查询单点（自动归并墓碑）的条数
func (r *SurveyPointRepo) Counts(id int64) (models.MergePointCounts, error) {
	canonical, _, err := r.Resolve(id)
	if err != nil {
		return models.MergePointCounts{}, err
	}
	return r.canonicalCounts(canonical)
}

// ---- helpers ----

func (r *SurveyPointRepo) loadAliases(p *models.SurveyPoint) error {
	rows, err := r.db.Query(`SELECT name FROM survey_point_aliases WHERE point_id=$1 ORDER BY id`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		p.Aliases = append(p.Aliases, n)
	}
	return nil
}

func insertAliases(tx *sql.Tx, pointID int64, names []string) error {
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		// 别名被别的点占用 = 潜在异名同地，当场拒绝并提示去认重/合并
		var owner int64
		err := tx.QueryRow(`SELECT point_id FROM survey_point_aliases WHERE name=$1`, n).Scan(&owner)
		if err == nil && owner != pointID {
			return fmt.Errorf("%w: alias %q already belongs to point %d", ErrAliasNameConflict, n, owner)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO survey_point_aliases (point_id, name) VALUES ($1,$2)
			 ON CONFLICT (point_id, name) DO NOTHING`, pointID, n); err != nil {
			return err
		}
	}
	return nil
}

func regionLeafInTx(tx *sql.Tx, regionID int64) (bool, error) {
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM dialect_regions WHERE id=$1`, regionID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	var child int64
	err := tx.QueryRow(`SELECT id FROM dialect_regions WHERE parent_id=$1 LIMIT 1`, regionID).Scan(&child)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func qualify(cols, alias string) string {
	parts := strings.Split(cols, ", ")
	for i, c := range parts {
		parts[i] = alias + "." + c
	}
	return strings.Join(parts, ", ")
}
