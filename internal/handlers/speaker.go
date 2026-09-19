package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type SpeakerHandler struct {
	repo      *repository.SpeakerRepo
	pointRepo *repository.SurveyPointRepo
}

func NewSpeakerHandler(repo *repository.SpeakerRepo, pointRepo *repository.SurveyPointRepo) *SpeakerHandler {
	return &SpeakerHandler{repo: repo, pointRepo: pointRepo}
}

func (h *SpeakerHandler) Create(c *gin.Context) {
	var req models.CreateSpeakerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	// 挂调查点时，必须是现存、未被合并的档案
	if req.SurveyPointID != nil {
		point, err := h.pointRepo.GetByID(*req.SurveyPointID)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "survey_point_id not found", Detail: err.Error()})
			return
		}
		if point.MergedInto != nil {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Code:    409,
				Message: "survey point has been merged; attach to the surviving point instead",
				Detail:  "canonical point_id=" + strconv.FormatInt(*point.MergedInto, 10),
			})
			return
		}
	}

	speaker := &models.Speaker{
		CodeName:         req.CodeName,
		BirthYear:        req.BirthYear,
		Gender:           req.Gender,
		DialectPointCode: req.DialectPointCode,
		Occupation:       req.Occupation,
		YearsAway:        req.YearsAway,
		ContactRef:       req.ContactRef,
		SurveyPointID:    req.SurveyPointID,
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