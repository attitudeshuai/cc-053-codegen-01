package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

// isUniqueViolation 是否唯一约束冲突（code 重复等）
func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

type ArchiveHandler struct {
	countyRepo *repository.CountyRepo
	regionRepo *repository.RegionRepo
	pointRepo  *repository.SurveyPointRepo
}

func NewArchiveHandler(
	countyRepo *repository.CountyRepo,
	regionRepo *repository.RegionRepo,
	pointRepo *repository.SurveyPointRepo,
) *ArchiveHandler {
	return &ArchiveHandler{countyRepo: countyRepo, regionRepo: regionRepo, pointRepo: pointRepo}
}

// mapArchiveError 档案类错误 → HTTP 状态
func mapArchiveError(c *gin.Context, err error, action string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, repository.ErrNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "referenced archive entity not found", Detail: err.Error()})
	case errors.Is(err, repository.ErrDoubleAssignment),
		errors.Is(err, repository.ErrAlreadyMerged),
		errors.Is(err, repository.ErrNameConflict),
		errors.Is(err, repository.ErrAliasNameConflict),
		errors.Is(err, repository.ErrSamePoint),
		errors.Is(err, repository.ErrCountyMismatch):
		// 409：挂重 / 已合并 / 重名 / 跨县合并等需要当场指出的冲突
		c.JSON(http.StatusConflict, models.ErrorResponse{Code: 409, Message: action + " conflict", Detail: err.Error()})
	case errors.Is(err, repository.ErrRegionNotLeaf),
		errors.Is(err, repository.ErrLevelGap),
		errors.Is(err, repository.ErrCycle):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid archive hierarchy", Detail: err.Error()})
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "not found"})
	default:
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, models.ErrorResponse{Code: 409, Message: action + " conflict: unique code/name already exists", Detail: err.Error()})
			return true
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: action + " failed", Detail: err.Error()})
	}
	return true
}

// ---- County ----

func (h *ArchiveHandler) CreateCounty(c *gin.Context) {
	var req models.CreateCountyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	county := &models.County{Name: req.Name, Code: req.Code, Province: req.Province}
	if err := h.countyRepo.Create(county); err != nil {
		mapArchiveError(c, err, "create county")
		return
	}
	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "county created", Data: county})
}

func (h *ArchiveHandler) GetCounty(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	county, err := h.countyRepo.GetByID(id)
	if mapArchiveError(c, err, "get county") {
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: county})
}

func (h *ArchiveHandler) ListCounties(c *gin.Context) {
	var p models.Pagination
	_ = c.ShouldBindQuery(&p)
	p.Normalize()
	items, total, err := h.countyRepo.List(p.Offset, p.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "list counties failed", Detail: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": items, "total": total, "offset": p.Offset, "limit": p.Limit}})
}

// ---- DialectRegion ----

func (h *ArchiveHandler) CreateRegion(c *gin.Context) {
	var req models.CreateRegionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	region := &models.DialectRegion{Name: req.Name, Level: req.Level, ParentID: req.ParentID}
	if err := h.regionRepo.Create(region); err != nil {
		mapArchiveError(c, err, "create region")
		return
	}
	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "region created", Data: region})
}

func (h *ArchiveHandler) GetRegion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	region, err := h.regionRepo.GetByID(id)
	if mapArchiveError(c, err, "get region") {
		return
	}
	path, _ := h.regionRepo.PathOf(id)
	leaf, _ := h.regionRepo.IsLeaf(id)
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: models.RegionNode{
		DialectRegion: *region, Path: path, IsLeaf: leaf,
	}})
}

func (h *ArchiveHandler) ListRegions(c *gin.Context) {
	items, err := h.regionRepo.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "list regions failed", Detail: err.Error()})
		return
	}
	// 标记叶子
	childSet := map[int64]bool{}
	for _, r := range items {
		if r.ParentID != nil {
			childSet[*r.ParentID] = true
		}
	}
	out := make([]models.RegionNode, 0, len(items))
	for _, r := range items {
		out = append(out, models.RegionNode{DialectRegion: *r, IsLeaf: !childSet[r.ID]})
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": out, "total": len(out)}})
}

// ---- SurveyPoint ----

func (h *ArchiveHandler) CreatePoint(c *gin.Context) {
	var req models.CreateSurveyPointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	// 认重：本名（同县）或别名（跨县）已被现存档案占用 → 当场指出，给出既有档案与合并入口
	for _, candidate := range append([]string{req.Name}, req.Aliases...) {
		if existing, err := h.pointRepo.FindConflict(candidate, req.CountyID); err == nil && existing != nil {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Code:    409,
				Message: "possible duplicate: a survey point with this name/alias already exists; use POST /api/v1/survey-points/merge if they are the same place",
				Detail:  "existing point_id=" + strconv.FormatInt(existing.ID, 10) + " name=" + existing.Name,
			})
			return
		}
	}

	point := &models.SurveyPoint{
		Name:     req.Name,
		Code:     req.Code,
		CountyID: req.CountyID,
		Aliases:  req.Aliases,
		Note:     req.Note,
	}
	if err := h.pointRepo.Create(point, req.RegionID); err != nil {
		mapArchiveError(c, err, "create survey point")
		return
	}
	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "survey point created", Data: point})
}

func (h *ArchiveHandler) GetPoint(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	detail, err := h.pointRepo.Detail(id)
	if mapArchiveError(c, err, "get survey point") {
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: detail})
}

func (h *ArchiveHandler) ListPoints(c *gin.Context) {
	var f struct {
		CountyID      int64  `form:"county_id"`
		RegionID      int64  `form:"region_id"`
		Q             string `form:"q"`
		IncludeMerged bool   `form:"include_merged"`
		models.Pagination
	}
	if err := c.ShouldBindQuery(&f); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid query", Detail: err.Error()})
		return
	}
	f.Pagination.Normalize()
	items, total, err := h.pointRepo.List(repository.PointFilter{
		CountyID:   f.CountyID,
		RegionID:   f.RegionID,
		Q:          f.Q,
		OnlyLive:   !f.IncludeMerged,
		Pagination: f.Pagination,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "list survey points failed", Detail: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{
		"items": items, "total": total, "offset": f.Offset, "limit": f.Limit,
	}})
}

// AssignRegion 调整归属：必须写明从哪天起算（默认今天）
func (h *ArchiveHandler) AssignRegion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	var req models.AssignRegionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	validFrom := time.Now().UTC()
	if req.ValidFrom != "" {
		validFrom, err = time.Parse("2006-01-02", req.ValidFrom)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "valid_from must be YYYY-MM-DD", Detail: err.Error()})
			return
		}
	}

	a, err := h.pointRepo.AssignRegion(id, req.RegionID, validFrom, req.ChangedBy, req.Reason)
	if mapArchiveError(c, err, "assign region") {
		return
	}
	c.JSON(http.StatusCreated, models.APIResponse{
		Code:    201,
		Message: "assignment recorded from " + validFrom.Format("2006-01-02") + "; earlier history is unchanged",
		Data:    a,
	})
}

func (h *ArchiveHandler) ListAssignments(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	items, err := h.pointRepo.ListAssignments(id)
	if mapArchiveError(c, err, "list assignments") {
		return
	}
	// 补上每条归属的分区链路
	for _, a := range items {
		a.RegionPath, _ = h.regionRepo.PathOf(a.RegionID)
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": items}})
}

// AssignmentAt 按当时归属回看：?at=YYYY-MM-DD
func (h *ArchiveHandler) AssignmentAt(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	at := time.Now().UTC()
	if raw := c.Query("at"); raw != "" {
		at, err = time.Parse("2006-01-02", raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "at must be YYYY-MM-DD"})
			return
		}
	}
	res, err := h.pointRepo.AssignmentAt(id, at)
	if mapArchiveError(c, err, "lookup assignment") {
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{
		Code:    200,
		Message: "point-in-time attribution; historical statements are not rewritten by later changes",
		Data:    res,
	})
}

// MergePoints 异名同地合并，返回条数对账
func (h *ArchiveHandler) MergePoints(c *gin.Context) {
	var req models.MergePointsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	result, err := h.pointRepo.MergePoints(req.KeptPointID, req.MergedPointID)
	if mapArchiveError(c, err, "merge points") {
		return
	}
	status := http.StatusOK
	msg := "survey points merged; counts balanced"
	if !result.Balanced {
		// 条数对不上：不静默成功，明确报出
		status = http.StatusConflict
		msg = "merge finished but counts are NOT balanced; inspect consistency_note"
	}
	c.JSON(status, models.APIResponse{Code: status, Message: msg, Data: result})
}

// PointCounts 查询单点（自动归并已合入档案）的关联条数
func (h *ArchiveHandler) PointCounts(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	counts, err := h.pointRepo.Counts(id)
	if mapArchiveError(c, err, "counts") {
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: counts})
}
