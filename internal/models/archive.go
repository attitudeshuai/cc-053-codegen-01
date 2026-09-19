package models

import "time"

// County 县（调查点的行政归属）
type County struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Province  string    `json:"province,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DialectRegion 方言分区，多级树。Level: 1=大区，2=片，3=小片……
// 调查点只能挂在叶子节点（最细一级）上。
type DialectRegion struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Level     int       `json:"level"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RegionNode 带层级路径信息的分区节点，供树形展示
type RegionNode struct {
	DialectRegion
	Path   []RegionBrief `json:"path"`
	IsLeaf bool          `json:"is_leaf"`
}

// RegionBrief 分区链路上的简要节点
type RegionBrief struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// SurveyPoint 方言调查点档案
type SurveyPoint struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Code            string    `json:"code"`
	CountyID        int64     `json:"county_id"`
	CurrentRegionID *int64    `json:"current_region_id,omitempty"`
	MergedInto      *int64    `json:"merged_into,omitempty"`
	Note            string    `json:"note,omitempty"`
	Aliases         []string  `json:"aliases,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SurveyPointDetail 调查点详情：含县名、分区链路、别名、当前归属
type SurveyPointDetail struct {
	SurveyPoint
	CountyName string        `json:"county_name"`
	CountyCode string        `json:"county_code"`
	RegionPath []RegionBrief `json:"region_path,omitempty"`
}

// Assignment 一次归属记录（时间线上的一行）
type Assignment struct {
	ID        int64      `json:"id"`
	PointID   int64      `json:"point_id"`
	RegionID  int64      `json:"region_id"`
	ValidFrom time.Time  `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	ChangedBy string     `json:"changed_by,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// AssignmentView 归属记录 + 分区链路
type AssignmentView struct {
	Assignment
	RegionName string        `json:"region_name"`
	RegionPath []RegionBrief `json:"region_path,omitempty"`
}

// AssignmentAt 任一时点回看归属的结果
type AssignmentAt struct {
	PointID    int64         `json:"point_id"`
	PointName  string        `json:"point_name"`
	At         time.Time     `json:"at"`
	RegionID   *int64        `json:"region_id,omitempty"`
	RegionName string        `json:"region_name,omitempty"`
	RegionPath []RegionBrief `json:"region_path,omitempty"`
}

// MergeResult 合并结果：含条数对账
type MergeResult struct {
	KeptPointID        int64            `json:"kept_point_id"`
	MergedPointID      int64            `json:"merged_point_id"`
	KeptName           string           `json:"kept_name"`
	KeptCountsBefore   MergePointCounts `json:"kept_counts_before"`
	MergedCountsBefore MergePointCounts `json:"merged_counts_before"`
	CountsBefore       MergePointCounts `json:"counts_before"` // 两边合计
	CountsAfter        MergePointCounts `json:"counts_after"`  // 合并后保留点（含墓碑）统算
	AliasesMoved       []string         `json:"aliases_moved"`
	AssignmentRows     int64            `json:"assignment_rows_moved"`
	Balanced           bool             `json:"balanced"`
	ConsistencyNote    string           `json:"consistency_note"`
}

// MergePointCounts 调查点关联条数，合并前后逐项对账
type MergePointCounts struct {
	SurveyPoints int64 `json:"survey_points"` // 保留点恒为 1（含被并入点时为 2）
	Aliases      int64 `json:"aliases"`
	Speakers     int64 `json:"speakers"`
	Tasks        int64 `json:"tasks"`
	Recordings   int64 `json:"recordings"`
	Segments     int64 `json:"segments"`
	Annotations  int64 `json:"annotations"`
	Assignments  int64 `json:"assignments"`
}

// ---- 请求结构 ----

type CreateCountyRequest struct {
	Name     string `json:"name" binding:"required"`
	Code     string `json:"code" binding:"required"`
	Province string `json:"province"`
}

type CreateRegionRequest struct {
	Name     string `json:"name" binding:"required"`
	Level    int    `json:"level" binding:"required,min=1"`
	ParentID *int64 `json:"parent_id"`
}

type CreateSurveyPointRequest struct {
	Name     string   `json:"name" binding:"required"`
	Code     string   `json:"code" binding:"required"`
	CountyID int64    `json:"county_id" binding:"required"`
	RegionID int64    `json:"region_id"` // 建点时的初始归属（叶子分区）
	Aliases  []string `json:"aliases"`
	Note     string   `json:"note"`
}

// AssignRegionRequest 调整归属；valid_from 为生效日（YYYY-MM-DD），缺省为今天
type AssignRegionRequest struct {
	RegionID  int64  `json:"region_id" binding:"required"`
	ValidFrom string `json:"valid_from"`
	ChangedBy string `json:"changed_by"`
	Reason    string `json:"reason"`
}

// MergePointsRequest 把两个异名同地的档案合成一个
type MergePointsRequest struct {
	KeptPointID   int64 `json:"kept_point_id" binding:"required"`
	MergedPointID int64 `json:"merged_point_id" binding:"required"`
}
