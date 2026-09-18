package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type SegmentHandler struct {
	repo *repository.SegmentRepo
}

func NewSegmentHandler(repo *repository.SegmentRepo) *SegmentHandler {
	return &SegmentHandler{repo: repo}
}

func (h *SegmentHandler) List(c *gin.Context) {
	var query models.SegmentQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid query", Detail: err.Error()})
		return
	}

	segments, total, err := h.repo.List(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list segments", Detail: err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": segments, "total": total, "offset": query.Offset, "limit": query.Limit}})
}