package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type SpeakerHandler struct {
	repo *repository.SpeakerRepo
}

func NewSpeakerHandler(repo *repository.SpeakerRepo) *SpeakerHandler {
	return &SpeakerHandler{repo: repo}
}

func (h *SpeakerHandler) Create(c *gin.Context) {
	var req models.CreateSpeakerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	speaker := &models.Speaker{
		CodeName:         req.CodeName,
		BirthYear:        req.BirthYear,
		Gender:           req.Gender,
		DialectPointCode: req.DialectPointCode,
		Occupation:       req.Occupation,
		YearsAway:        req.YearsAway,
		ContactRef:       req.ContactRef,
	}

	if err := h.repo.Create(speaker); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to create speaker", Detail: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "speaker created", Data: speaker})
}

func (h *SpeakerHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}

	speaker, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "speaker not found"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: speaker})
}

func (h *SpeakerHandler) List(c *gin.Context) {
	var p models.Pagination
	if err := c.ShouldBindQuery(&p); err != nil {
		p = models.Pagination{}
	}
	p.Normalize()

	speakers, total, err := h.repo.List(p.Offset, p.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list speakers"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": speakers, "total": total, "offset": p.Offset, "limit": p.Limit}})
}