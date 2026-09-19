package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

// respondBiz 把仓储层的业务错误翻译成合适的 HTTP 状态；其余按 500 处理
func respondBiz(c *gin.Context, err error, fallbackMsg string) {
	var biz *repository.BizError
	if !errors.As(err, &biz) {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: fallbackMsg, Detail: err.Error()})
		return
	}
	status := http.StatusBadRequest
	switch biz.Code {
	case "not_found":
		status = http.StatusNotFound
	case "conflict", "overlap", "not_leaf", "no_assignment", "merged":
		status = http.StatusConflict
	case "bad_input":
		status = http.StatusBadRequest
	}
	c.JSON(status, models.ErrorResponse{Code: status, Message: biz.Message, Detail: biz.Detail})
}

func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.UTC)
}
