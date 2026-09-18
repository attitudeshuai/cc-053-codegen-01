package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
	"cc-053/internal/services"
)

type AnnotationHandler struct {
	annotationRepo *repository.AnnotationRepo
	segmentRepo    *repository.SegmentRepo
}

func NewAnnotationHandler(annotationRepo *repository.AnnotationRepo, segmentRepo *repository.SegmentRepo) *AnnotationHandler {
	return &AnnotationHandler{
		annotationRepo: annotationRepo,
		segmentRepo:    segmentRepo,
	}
}

func (h *AnnotationHandler) Submit(c *gin.Context) {
	segmentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid segment id"})
		return
	}

	var req models.AnnotationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	// Get segment with optimistic lock
	segment, err := h.segmentRepo.GetByID(segmentID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "segment not found"})
		return
	}

	if req.Version != segment.Version {
		c.JSON(http.StatusConflict, models.ErrorResponse{Code: 409, Message: "optimistic lock conflict: segment has been updated"})
		return
	}

	// Check for existing annotations
	existingAnnots, err := h.annotationRepo.ListBySegment(segmentID)
	if err == nil && len(existingAnnots) > 0 {
		// Check if this annotator already submitted
		annotator := c.GetHeader("X-Annotator")
		if annotator == "" {
			annotator = "anonymous"
		}
		for _, ea := range existingAnnots {
			if ea.Annotator == annotator {
				c.JSON(http.StatusConflict, models.ErrorResponse{Code: 409, Message: "annotator already submitted for this segment"})
				return
			}
		}

		// Check consistency with first annotation
		if len(existingAnnots) == 1 {
			consistent, _ := services.CheckConsistency(req.IPA, existingAnnots[0].IPA)
			if !consistent {
				// Mark both as needing arbitration
				h.segmentRepo.UpdateStatus(segmentID, "in_arbitration", segment.Version)
			}
		}
	}

	annotator := c.GetHeader("X-Annotator")
	if annotator == "" {
		annotator = "anonymous"
	}

	annotation := &models.Annotation{
		SegmentID: segmentID,
		Annotator: annotator,
		IPA:       req.IPA,
		Tone:      req.Tone,
		Note:      req.Note,
	}

	if err := h.annotationRepo.Create(annotation); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to create annotation", Detail: err.Error()})
		return
	}

	// Update segment status
	annotCount, _ := h.annotationRepo.CountBySegment(segmentID)
	_ = annotCount

	// Check if we have 2 annotations now
	allAnnots, _ := h.annotationRepo.ListBySegment(segmentID)
	if len(allAnnots) >= 2 {
		// Check consistency between the two
		consistent, _ := services.CheckConsistency(allAnnots[0].IPA, allAnnots[1].IPA)
		if consistent {
			h.segmentRepo.UpdateStatus(segmentID, "annotated", segment.Version)
			h.annotationRepo.UpdateDecision(allAnnots[0].ID, "accept")
			h.annotationRepo.UpdateDecision(allAnnots[1].ID, "accept")
		} else {
			h.segmentRepo.UpdateStatus(segmentID, "in_arbitration", segment.Version)
		}
	}

	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "annotation submitted", Data: annotation})
}