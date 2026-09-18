package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type TaskHandler struct {
	repo *repository.TaskRepo
}

func NewTaskHandler(repo *repository.TaskRepo) *TaskHandler {
	return &TaskHandler{repo: repo}
}

func (h *TaskHandler) Create(c *gin.Context) {
	var req models.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}

	task := &models.Task{
		WordlistID: req.WordlistID,
		SpeakerID:  req.SpeakerID,
		Kind:       req.Kind,
		Assignee:   req.Assignee,
		Status:     "pending",
	}

	if err := h.repo.Create(task); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to create task", Detail: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "task created", Data: task})
}

func (h *TaskHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}

	task, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Code: 404, Message: "task not found"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: task})
}

func (h *TaskHandler) List(c *gin.Context) {
	var p models.Pagination
	if err := c.ShouldBindQuery(&p); err != nil {
		p = models.Pagination{}
	}
	p.Normalize()

	tasks, total, err := h.repo.List(p.Offset, p.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list tasks"})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": tasks, "total": total, "offset": p.Offset, "limit": p.Limit}})
}