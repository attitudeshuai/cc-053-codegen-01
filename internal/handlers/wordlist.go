package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type WordlistHandler struct {
	repo *repository.WordlistRepo
}

func NewWordlistHandler(repo *repository.WordlistRepo) *WordlistHandler {
	return &WordlistHandler{repo: repo}
}

func (h *WordlistHandler) Create(c *gin.Context) {
	var req models.CreateWordlistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	wordlist := &models.Wordlist{
		Name:    req.Name,
		Entries: req.Entries,
	}

	if err := h.repo.Create(wordlist); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to create wordlist", Detail: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "wordlist created", Data: wordlist})
}

func (h *WordlistHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}

	wordlist, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "wordlist not found"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: wordlist})
}

func (h *WordlistHandler) List(c *gin.Context) {
	var p models.Pagination
	if err := c.ShouldBindQuery(&p); err != nil {
		p = models.Pagination{}
	}
	p.Normalize()

	wordlists, total, err := h.repo.List(p.Offset, p.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list wordlists"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": wordlists, "total": total, "offset": p.Offset, "limit": p.Limit}})
}