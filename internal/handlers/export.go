package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
	"cc-053/internal/services"
)

type ExportHandler struct {
	exportRepo  *repository.ExportRepo
	exportSvc   *services.ExportService
}

func NewExportHandler(exportRepo *repository.ExportRepo, exportSvc *services.ExportService) *ExportHandler {
	return &ExportHandler{
		exportRepo: exportRepo,
		exportSvc:  exportSvc,
	}
}

func (h *ExportHandler) Create(c *gin.Context) {
	var req models.CreateExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	job := &models.ExportJob{
		Status: "pending",
	}

	if err := h.exportRepo.Create(job); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to create export job", Detail: err.Error()})
		return
	}

	// Trigger async export (simplified - would be async in production)
	// For MVP, run synchronously
	go func() {
		h.exportRepo.UpdateProgress(job.ID, 0, "processing", "", "")
		outputKey, err := h.exportSvc.GenerateExport(c.Request.Context(), job.ID, req)
		if err != nil {
			h.exportRepo.UpdateProgress(job.ID, 0, "failed", "", err.Error())
			return
		}
		h.exportRepo.UpdateProgress(job.ID, 100, "completed", outputKey, "")
	}()

	c.JSON(http.StatusAccepted, models.APIResponse{Code: 202, Message: "export job created", Data: job})
}

func (h *ExportHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}

	job, err := h.exportRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "export job not found"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: job})
}

func (h *ExportHandler) List(c *gin.Context) {
	var p models.Pagination
	if err := c.ShouldBindQuery(&p); err != nil {
		p = models.Pagination{}
	}
	p.Normalize()

	jobs, total, err := h.exportRepo.List(p.Offset, p.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list export jobs"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": jobs, "total": total, "offset": p.Offset, "limit": p.Limit}})
}