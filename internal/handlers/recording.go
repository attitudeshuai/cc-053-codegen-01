package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
	"cc-053/internal/services"
)

type RecordingHandler struct {
	recordingRepo  *repository.RecordingRepo
	taskRepo       *repository.TaskRepo
	minioSvc       *services.MinIOService
}

func NewRecordingHandler(recordingRepo *repository.RecordingRepo, taskRepo *repository.TaskRepo, minioSvc *services.MinIOService) *RecordingHandler {
	return &RecordingHandler{
		recordingRepo: recordingRepo,
		taskRepo:      taskRepo,
		minioSvc:      minioSvc,
	}
}

func (h *RecordingHandler) GetUploadURL(c *gin.Context) {
	var req models.UploadURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	// Verify task exists
	task, err := h.taskRepo.GetByID(req.TaskID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "task not found"})
		return
	}
	if task.Kind != "record" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "task is not a recording task"})
		return
	}

	objectKey := "recordings/" + strconv.FormatInt(req.TaskID, 10) + "/" + req.Filename
	url, err := h.minioSvc.PresignedPutURL(c.Request.Context(), objectKey, 30*time.Minute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to generate upload URL", Detail: err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: models.UploadURLResponse{
		URL:       url,
		ObjectKey: objectKey,
		ExpiresIn: 1800,
	}})
}

func (h *RecordingHandler) Create(c *gin.Context) {
	var req models.CreateRecordingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	var recordedAt *time.Time
	if req.RecordedAt != "" {
		t, err := time.Parse(time.RFC3339, req.RecordedAt)
		if err == nil {
			recordedAt = &t
		}
	}

	recording := &models.Recording{
		TaskID:     req.TaskID,
		ObjectKey:  req.ObjectKey,
		DurationMs: req.DurationMs,
		SampleRate: req.SampleRate,
		PeakDB:     req.PeakDB,
		Device:     req.Device,
		RecordedAt: recordedAt,
		Status:     "pending",
	}

	if err := h.recordingRepo.Create(recording); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to create recording", Detail: err.Error()})
		return
	}

	// Trigger async processing via asynq (placeholder - will be handled by worker)
	// In production, enqueue a processing task here

	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "recording created", Data: recording})
}

func (h *RecordingHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}

	recording, err := h.recordingRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "recording not found"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: recording})
}