package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type ArbitrationHandler struct {
	arbitrationRepo *repository.ArbitrationRepo
	annotationRepo  *repository.AnnotationRepo
	segmentRepo     *repository.SegmentRepo
}

func NewArbitrationHandler(arbRepo *repository.ArbitrationRepo, annotRepo *repository.AnnotationRepo, segRepo *repository.SegmentRepo) *ArbitrationHandler {
	return &ArbitrationHandler{
		arbitrationRepo: arbRepo,
		annotationRepo:  annotRepo,
		segmentRepo:     segRepo,
	}
}

func (h *ArbitrationHandler) Arbitrate(c *gin.Context) {
	segmentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid segment id"})
		return
	}

	var req models.ArbitrateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	// Verify the winner annotation exists
	annotation, err := h.annotationRepo.GetByID(req.WinnerAnnotationID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "annotation not found"})
		return
	}
	if annotation.SegmentID != segmentID {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "annotation does not belong to this segment"})
		return
	}

	segment, err := h.segmentRepo.GetByID(segmentID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "segment not found"})
		return
	}

	arbitration := &models.Arbitration{
		SegmentID:          segmentID,
		WinnerAnnotationID: req.WinnerAnnotationID,
		Arbiter:            req.Arbiter,
		Reason:             req.Reason,
	}

	if err := h.arbitrationRepo.Create(arbitration); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to create arbitration", Detail: err.Error()})
		return
	}

	// Update annotation decisions
	allAnnots, _ := h.annotationRepo.ListBySegment(segmentID)
	for _, a := range allAnnots {
		if a.ID == req.WinnerAnnotationID {
			h.annotationRepo.UpdateDecision(a.ID, "arbitrated")
		} else {
			h.annotationRepo.UpdateDecision(a.ID, "reject")
		}
	}

	// Mark segment as completed
	h.segmentRepo.UpdateStatus(segmentID, "completed", segment.Version)

	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "arbitration created", Data: arbitration})
}