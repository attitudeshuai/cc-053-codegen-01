package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type SurveyPointHandler struct {
	repo *repository.SurveyPointRepo
}

func NewSurveyPointHandler(repo *repository.SurveyPointRepo) *SurveyPointHandler {
	return &SurveyPointHandler{repo: repo}
}

func (h *SurveyPointHandler) Create(c *gin.Context) {
	var req models.CreateSurveyPointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	p := &models.SurveyPoint{
		Name:     req.Name,
		Code:     req.Code,
		CountyID: req.CountyID,
		Note:     req.Note,
	}

	// 可选：建档同时挂到一个叶子区，需给出生效日
	var initialRegion *int64
	var effDate time.Time
	var createdBy string
	if req.RegionID != nil {
		if req.EffectiveDate == "" {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "挂接方言区必须同时提供 effective_date（YYYY-MM-DD）"})
			return
		}
		d, err := parseDate(req.EffectiveDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "effective_date 格式应为 YYYY-MM-DD", Detail: err.Error()})
			return
		}
		initialRegion, effDate, createdBy = req.RegionID, d, req.CreatedBy
	}

	if err := h.repo.Create(p, req.Aliases, initialRegion, effDate, createdBy); err != nil {
		respondBiz(c, err, "failed to create survey point")
		return
	}
	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "survey point created", Data: p})
}

func (h *SurveyPointHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	p, err := h.repo.GetByID(id)
	if err != nil {
		respondBiz(c, err, "failed to get survey point")
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: p})
}

func (h *SurveyPointHandler) List(c *gin.Context) {
	var q struct {
		CountyID int64  `form:"county_id"`
		RegionID int64  `form:"region_id"`
		Keyword  string `form:"keyword"`
		Status   string `form:"status"`
		models.Pagination
	}
	if err := c.ShouldBindQuery(&q); err != nil {
		q.Pagination = models.Pagination{}
	}
	q.Pagination.Normalize()

	items, total, err := h.repo.List(repository.SurveyPointFilter{
		CountyID: q.CountyID,
		RegionID: q.RegionID,
		Keyword:  q.Keyword,
		Status:   q.Status,
		Offset:   q.Offset,
		Limit:    q.Limit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list survey points"})
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{
		"items": items, "total": total, "offset": q.Offset, "limit": q.Limit,
	}})
}

// Adjust 调整归属：PUT /survey-points/:id/assignment
func (h *SurveyPointHandler) Adjust(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	var req models.AdjustAssignmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	eff, err := parseDate(req.EffectiveDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "effective_date 格式应为 YYYY-MM-DD", Detail: err.Error()})
		return
	}
	a, err := h.repo.AdjustAssignment(id, req.RegionID, eff, req.CreatedBy, req.Note)
	if err != nil {
		respondBiz(c, err, "failed to adjust assignment")
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Message: "assignment adjusted", Data: a})
}

// Assignments 归属时间线：GET /survey-points/:id/assignments
func (h *SurveyPointHandler) Assignments(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	asg, err := h.repo.Assignments(id)
	if err != nil {
		respondBiz(c, err, "failed to list assignments")
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: asg})
}

// AssignmentAt 按日期回看当时归属：GET /survey-points/:id/assignment-at?date=
func (h *SurveyPointHandler) AssignmentAt(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	d, err := parseDate(c.Query("date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "date 格式应为 YYYY-MM-DD"})
		return
	}
	a, err := h.repo.AssignmentAt(id, d)
	if err != nil {
		respondBiz(c, err, "failed to lookup assignment")
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "该日期没有归属记录"})
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: a})
}

// CreateRecord 登记一条已发出的说法（按当时归属固化快照）
func (h *SurveyPointHandler) CreateRecord(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	var req models.CreatePointRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	d, err := parseDate(req.RecordDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "record_date 格式应为 YYYY-MM-DD", Detail: err.Error()})
		return
	}
	rec, err := h.repo.CreateRecord(id, req.Content, d, req.CreatedBy)
	if err != nil {
		respondBiz(c, err, "failed to create record")
		return
	}
	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "record created", Data: rec})
}

// History 档案 + 归属时间线 + 说法
func (h *SurveyPointHandler) History(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	hist, err := h.repo.History(id)
	if err != nil {
		respondBiz(c, err, "failed to load history")
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: hist})
}

// Merge 把两个其实是同一地方的档案合成一个
func (h *SurveyPointHandler) Merge(c *gin.Context) {
	var req models.MergePointsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	m, err := h.repo.Merge(req.SurvivingID, req.MergedID, req.MergedBy, req.Note)
	if err != nil {
		respondBiz(c, err, "failed to merge survey points")
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Message: "survey points merged", Data: m})
}

// MergeHistory 合并审计
func (h *SurveyPointHandler) MergeHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, err := h.repo.MergeHistory(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list merges"})
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: items})
}
