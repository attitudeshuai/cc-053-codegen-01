package models

import "time"

// County 县（行政区划，调查点归属）
type County struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// DialectRegion 多级方言区。Level 越小层级越高（1=一级/大区）；
// ParentID 为 nil 的是一级区；没有任何下级的才是“叶子区”，调查点只能挂叶子区。
type DialectRegion struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Code     string  `json:"code"`
	Level    int     `json:"level"`
	ParentID *int64  `json:"parent_id,omitempty"`
	Path     []int64 `json:"path"`
	Note     string  `json:"note,omitempty"`
	// IsLeaf 由查询时统计子区得到（邻接表本身存不住这个标记）
	IsLeaf    bool      `json:"is_leaf"`
	CreatedAt time.Time `json:"created_at"`
}

// SurveyPoint 调查点档案。属哪个叶子区由 point_assignments 时间线表达，
// 这里只通过 Current* / AsOf* 字段展示归属，归属历史单独查。
type SurveyPoint struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Code         string   `json:"code"`
	CountyID     int64    `json:"county_id"`
	Status       string   `json:"status"` // active | merged
	MergedIntoID *int64   `json:"merged_into_id,omitempty"`
	Note         string   `json:"note,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	// 现行归属（叶子区）；尚未挂区时为 nil
	CurrentRegionID   *int64    `json:"current_region_id,omitempty"`
	CurrentRegion     string    `json:"current_region,omitempty"`
	CurrentRegionPath string    `json:"current_region_path,omitempty"`
	CountyName        string    `json:"county_name,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Assignment 一段归属区间 [EffectiveDate, EndDate)，EndDate 为零时间表示 infinity/现行
type Assignment struct {
	ID            int64      `json:"id"`
	PointID       int64      `json:"point_id"`
	RegionID      int64      `json:"region_id"`
	RegionName    string     `json:"region_name,omitempty"`
	RegionPath    string     `json:"region_path,omitempty"`
	EffectiveDate time.Time  `json:"effective_date"`
	EndDate       *time.Time `json:"end_date,omitempty"` // nil = 现行
	CreatedBy     string     `json:"created_by,omitempty"`
	Note          string     `json:"note,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// PointRecord 已发出的一条说法/记录。带建立当时的归属快照，之后永不改变。
type PointRecord struct {
	ID             int64     `json:"id"`
	PointID        int64     `json:"point_id"`
	Content        string    `json:"content"`
	RecordDate     time.Time `json:"record_date"`
	SnapshotCounty string    `json:"snapshot_county"`
	SnapshotRegion string    `json:"snapshot_region"`
	SnapshotPath   string    `json:"snapshot_path"`
	CreatedBy      string    `json:"created_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// PointMerge 合并审计记录
type PointMerge struct {
	ID               int64     `json:"id"`
	SurvivingPointID int64     `json:"surviving_point_id"`
	MergedPointID    int64     `json:"merged_point_id"`
	SurvivingBefore  int       `json:"surviving_before"`
	MergedBefore     int       `json:"merged_before"`
	TotalAfter       int       `json:"total_after"`
	MergedBy         string    `json:"merged_by,omitempty"`
	Note             string    `json:"note,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// ---------- 请求 / 响应 ----------

type CreateCountyRequest struct {
	Name string `json:"name" binding:"required"`
	Code string `json:"code" binding:"required"`
	Note string `json:"note"`
}

type CreateRegionRequest struct {
	Name     string `json:"name" binding:"required"`
	Code     string `json:"code" binding:"required"`
	Level    int    `json:"level"`
	ParentID *int64 `json:"parent_id"`
	Note     string `json:"note"`
}

type CreateSurveyPointRequest struct {
	Name     string   `json:"name" binding:"required"`
	Code     string   `json:"code" binding:"required"`
	CountyID int64    `json:"county_id" binding:"required"`
	Aliases  []string `json:"aliases"`
	Note     string   `json:"note"`
	// 可选：建档同时挂到一个叶子区（必须给 EffectiveDate）
	RegionID      *int64 `json:"region_id"`
	EffectiveDate string `json:"effective_date"` // YYYY-MM-DD
	CreatedBy     string `json:"created_by"`
}

// AdjustAssignmentRequest 调整归属：必须写明从哪天起算；目标必须是叶子区。
type AdjustAssignmentRequest struct {
	RegionID      int64  `json:"region_id" binding:"required"`
	EffectiveDate string `json:"effective_date" binding:"required"` // YYYY-MM-DD
	CreatedBy     string `json:"created_by"`
	Note          string `json:"note"`
}

type CreatePointRecordRequest struct {
	Content    string `json:"content" binding:"required"`
	RecordDate string `json:"record_date" binding:"required"` // YYYY-MM-DD
	CreatedBy  string `json:"created_by"`
}

type MergePointsRequest struct {
	// SurvivingID 保留的档案；MergedID 被并入、标记 merged。
	SurvivingID int64  `json:"surviving_id" binding:"required"`
	MergedID    int64  `json:"merged_id" binding:"required"`
	MergedBy    string `json:"merged_by"`
	Note        string `json:"note"`
}

// PointHistoryResponse 按时间线回看一个点的归属与记录
type PointHistoryResponse struct {
	Point       *SurveyPoint   `json:"point"`
	Assignments []*Assignment  `json:"assignments"` // 按生效日倒序
	Records     []*PointRecord `json:"records"`     // 按记录日期倒序
}
