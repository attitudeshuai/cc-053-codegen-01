package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type CountyHandler struct {
	repo *repository.CountyRepo
}

func NewCountyHandler(repo *repository.CountyRepo) *CountyHandler {
	return &CountyHandler{repo: repo}
}

func (h *CountyHandler) Create(c *gin.Context) {
	var req models.CreateCountyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	county := &models.County{Name: req.Name, Code: req.Code, Note: req.Note}
	if err := h.repo.Create(county); err != nil {
		respondBiz(c, err, "failed to create county")
		return
	}
	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "county created", Data: county})
}

func (h *CountyHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	county, err := h.repo.GetByID(id)
	if err != nil {
		respondBiz(c, err, "failed to get county")
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: county})
}

func (h *CountyHandler) List(c *gin.Context) {
	var p models.Pagination
	_ = c.ShouldBindQuery(&p)
	p.Normalize()
	items, total, err := h.repo.List(p.Offset, p.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list counties"})
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": items, "total": total, "offset": p.Offset, "limit": p.Limit}})
}
