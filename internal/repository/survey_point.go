package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"cc-053/internal/models"
)

type SurveyPointRepo struct {
	db       *sql.DB
	regions  *RegionRepo
	counties *CountyRepo
}

func NewSurveyPointRepo(db *sql.DB, regions *RegionRepo, counties *CountyRepo) *SurveyPointRepo {
	return &SurveyPointRepo{db: db, regions: regions, counties: counties}
}

const pointColumns = `id, name, code, county_id, status, merged_into_id, note, created_at, updated_at`

func scanPoint(s interface{ Scan(dest ...any) error }, p *models.SurveyPoint) error {
	var merged sql.NullInt64
	if err := s.Scan(
		&p.ID, &p.Name, &p.Code, &p.CountyID, &p.Status, &merged, &p.Note, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return err
	}
	if merged.Valid {
		v := merged.Int64
		p.MergedIntoID = &v
	}
	return nil
}

// aliasOwner 找到占用某个指纹的调查点（用于当场指认重复建档）
type aliasOwner struct {
	PointID int64
	Name    string
	Alias   string
}

func (r *SurveyPointRepo) findAliasOwner(tx *sql.Tx, fps []string) (*aliasOwner, error) {
	if len(fps) == 0 {
		return nil, nil
	}
	o := &aliasOwner{}
	err := tx.QueryRow(
		`SELECT a.point_id, p.name, a.name
		 FROM survey_point_aliases a JOIN survey_points p ON p.id = a.point_id
		 WHERE a.fingerprint = ANY($1) LIMIT 1`, pq.Array(fps),
	).Scan(&o.PointID, &o.Name, &o.Alias)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return o, nil
}

// Create 建调查点档案。name 与每个 alias 都算指纹，任一与既有档案重名即当场拒绝；
// initialRegion 非空时同时建立从 effectiveDate 起的首段归属，目标必须是叶子区。
func (r *SurveyPointRepo) Create(p *models.SurveyPoint, aliases []string, initialRegion *int64, effectiveDate time.Time, createdBy string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 县必须存在
	var ok bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM counties WHERE id=$1)`, p.CountyID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return bizErr("not_found", "所属县不存在", fmt.Sprintf("county_id=%d", p.CountyID))
	}

	// 名字去重（规范名 + 别名，请求内部先按指纹去重）
	seen := map[string]bool{}
	fps := []string{}
	addFp := func(nm string) {
		fp := NormalizeName(nm)
		if fp == "" || seen[fp] {
			return
		}
		seen[fp] = true
		fps = append(fps, fp)
	}
	addFp(p.Name)
	canonicalFp := NormalizeName(p.Name)
	aliasRows := [][2]string{} // (原始写法, 指纹)
	keptAlias := []string{}
	for _, a := range aliases {
		fp := NormalizeName(a)
		if fp == "" || seen[fp] {
			continue
		}
		seen[fp] = true
		fps = append(fps, fp)
		aliasRows = append(aliasRows, [2]string{a, fp})
		keptAlias = append(keptAlias, a)
	}

	owner, err := r.findAliasOwner(tx, fps)
	if err != nil {
		return err
	}
	if owner != nil {
		return bizErr("conflict", "该地名已存在档案，疑似同一地方重复建档",
			fmt.Sprintf("与调查点 #%d「%s」的名字「%s」重合", owner.PointID, owner.Name, owner.Alias))
	}

	if err = tx.QueryRow(
		`INSERT INTO survey_points (name, code, county_id, note)
		 VALUES ($1,$2,$3,$4) RETURNING id, created_at, updated_at`,
		p.Name, p.Code, p.CountyID, p.Note,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if isUnique(err) {
			return bizErr("conflict", "调查点编码已存在", p.Code)
		}
		return err
	}

	// 规范名作为 canonical 别名
	if _, err = tx.Exec(
		`INSERT INTO survey_point_aliases (point_id, name, fingerprint, source) VALUES ($1,$2,$3,'canonical')`,
		p.ID, p.Name, canonicalFp); err != nil {
		if isUnique(err, "ux_aliases_fingerprint") {
			// 并发情况下另一档案刚占用了同一名字指纹
			return bizErr("conflict", "该地名已存在档案，疑似同一地方重复建档", p.Name)
		}
		return err
	}
	for _, ar := range aliasRows {
		if _, err = tx.Exec(
			`INSERT INTO survey_point_aliases (point_id, name, fingerprint, source) VALUES ($1,$2,$3,'manual')`,
			p.ID, ar[0], ar[1]); err != nil {
			if isUnique(err, "ux_aliases_fingerprint") {
				return bizErr("conflict", "别名与既有调查点重名", ar[0])
			}
			return err
		}
	}

	if initialRegion != nil {
		if err := requireLeaf(tx, *initialRegion); err != nil {
			return err
		}
		if _, err = tx.Exec(
			`INSERT INTO point_assignments (point_id, region_id, effective_date, end_date, created_by)
			 VALUES ($1,$2,$3,NULL,$4)`,
			p.ID, *initialRegion, effectiveDate, createdBy); err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	p.Aliases = append([]string{p.Name}, keptAlias...)
	return nil
}

// requireLeaf 校验区存在且为叶子（没有下级）
func requireLeaf(tx *sql.Tx, regionID int64) error {
	var exists, hasChild bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM dialect_regions WHERE id=$1)`, regionID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return bizErr("not_found", "方言区不存在", fmt.Sprintf("region_id=%d", regionID))
	}
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM dialect_regions WHERE parent_id=$1)`, regionID).Scan(&hasChild); err != nil {
		return err
	}
	if hasChild {
		return bizErr("not_leaf", "调查点只能挂在最末一级方言区上，该区还有下级",
			fmt.Sprintf("region_id=%d", regionID))
	}
	return nil
}

func (r *SurveyPointRepo) loadAliases(tx *sql.Tx, pointID int64) ([]string, error) {
	rows, err := tx.Query(
		`SELECT name FROM survey_point_aliases WHERE point_id=$1 ORDER BY source DESC, id`, pointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var nm string
		if err = rows.Scan(&nm); err != nil {
			return nil, err
		}
		out = append(out, nm)
	}
	return out, nil
}

func (r *SurveyPointRepo) loadCurrent(tx *sql.Tx, pointID int64) (*models.Assignment, error) {
	a := &models.Assignment{}
	var regionID int64
	err := tx.QueryRow(
		`SELECT id, region_id, effective_date, created_by, note, created_at
		 FROM point_assignments WHERE point_id=$1 AND end_date IS NULL`, pointID,
	).Scan(&a.ID, &regionID, &a.EffectiveDate, &a.CreatedBy, &a.Note, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.PointID = pointID
	a.RegionID = regionID
	leafName, pathText, err := regionNamePathTx(tx, regionID)
	if err != nil {
		return nil, err
	}
	a.RegionName = leafName
	a.RegionPath = pathText
	return a, nil
}

func (r *SurveyPointRepo) enrich(tx *sql.Tx, p *models.SurveyPoint) error {
	aliases, err := r.loadAliases(tx, p.ID)
	if err != nil {
		return err
	}
	p.Aliases = aliases
	if err = tx.QueryRow(`SELECT name FROM counties WHERE id=$1`, p.CountyID).Scan(&p.CountyName); err != nil {
		return err
	}
	cur, err := r.loadCurrent(tx, p.ID)
	if err != nil {
		return err
	}
	if cur != nil {
		rid := cur.RegionID
		p.CurrentRegionID = &rid
		p.CurrentRegion = cur.RegionName
		p.CurrentRegionPath = cur.RegionPath
	}
	return nil
}

func (r *SurveyPointRepo) GetByID(id int64) (*models.SurveyPoint, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p := &models.SurveyPoint{}
	if err = scanPoint(tx.QueryRow(`SELECT `+pointColumns+` FROM survey_points WHERE id=$1`, id), p); err != nil {
		if err == sql.ErrNoRows {
			return nil, bizErr("not_found", "调查点不存在", "")
		}
		return nil, err
	}
	if err = r.enrich(tx, p); err != nil {
		return nil, err
	}
	return p, tx.Commit()
}

// SurveyPointFilter 列表过滤
type SurveyPointFilter struct {
	CountyID int64  // 归属县
	RegionID int64  // 当前归属区（含其各级子区）
	Keyword  string // 名称/别名模糊
	Status   string
	Offset   int
	Limit    int
}

func (r *SurveyPointRepo) List(f SurveyPointFilter) ([]*models.SurveyPoint, int, error) {
	args := make([]any, 0, 8)
	where := make([]string, 0, 4)
	// add 追加一个条件：模板里的 $%d 按已累积参数自动连续编号
	add := func(tmpl string, vs ...any) {
		nums := make([]any, len(vs))
		for i := range vs {
			n := len(args) + 1
			nums[i] = n
			args = append(args, vs[i])
		}
		where = append(where, fmt.Sprintf(tmpl, nums...))
	}
	if f.CountyID > 0 {
		add("p.county_id=$%d", f.CountyID)
	}
	if f.Status != "" {
		add("p.status=$%d", f.Status)
	}
	if f.RegionID > 0 {
		// 命中目标区本身或其任意下级：当前叶子区路径里含目标区 id。
		// 同一占位符在 PostgreSQL 里可重复引用，只需传一个参数。
		n := len(args) + 1
		args = append(args, f.RegionID)
		where = append(where, fmt.Sprintf(`EXISTS(
			SELECT 1 FROM point_assignments a JOIN dialect_regions rr ON rr.id=a.region_id
			WHERE a.point_id=p.id AND a.end_date IS NULL
			  AND (rr.id=$%d OR $%d = ANY(rr.path)))`, n, n))
	}
	if kw := NormalizeName(f.Keyword); kw != "" {
		add(`EXISTS(SELECT 1 FROM survey_point_aliases a
			WHERE a.point_id=p.id AND a.fingerprint LIKE $%d)`, "%"+kw+"%")
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM survey_points p `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, f.Limit, f.Offset)
	rows, err := r.db.Query(
		`SELECT `+pointColumns+` FROM survey_points p `+whereSQL+
			` ORDER BY p.id DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.SurveyPoint
	for rows.Next() {
		p := &models.SurveyPoint{}
		if err = scanPoint(rows, p); err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	rows.Close()

	// 逐条补充县名/别名/现行归属（limit ≤ 100）
	for _, p := range out {
		if err = r.fillLight(p); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func (r *SurveyPointRepo) fillLight(p *models.SurveyPoint) error {
	if err := r.db.QueryRow(`SELECT name FROM counties WHERE id=$1`, p.CountyID).Scan(&p.CountyName); err != nil {
		return err
	}
	if err := r.db.QueryRow(
		`SELECT a.region_id FROM point_assignments a WHERE a.point_id=$1 AND a.end_date IS NULL`, p.ID,
	).Scan(&p.CurrentRegionID); err == sql.ErrNoRows {
		// 未挂区
	} else if err != nil {
		return err
	} else if p.CurrentRegionID != nil {
		name, path, err := r.regions.NamePath(*p.CurrentRegionID)
		if err != nil {
			return err
		}
		p.CurrentRegion = name
		p.CurrentRegionPath = path
	}
	return nil
}
