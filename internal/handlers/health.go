package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"cc-053/internal/models"
)

type HealthCheckFunc func(ctx context.Context) error

type HealthHandler struct {
	checkDB      HealthCheckFunc
	checkRedis   HealthCheckFunc
	checkMinIO   HealthCheckFunc
}

func NewHealthHandler(dbCheck, redisCheck, minioCheck HealthCheckFunc) *HealthHandler {
	return &HealthHandler{
		checkDB:    dbCheck,
		checkRedis: redisCheck,
		checkMinIO: minioCheck,
	}
}

func (h *HealthHandler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	resp := models.HealthResponse{
		Status: "ok",
	}

	var allHealthy = true

	if err := h.checkDB(ctx); err != nil {
		resp.Database = "unhealthy: " + err.Error()
		allHealthy = false
		log.Error().Err(err).Msg("health check: database failed")
	} else {
		resp.Database = "ok"
	}

	if err := h.checkRedis(ctx); err != nil {
		resp.Redis = "unhealthy: " + err.Error()
		allHealthy = false
		log.Error().Err(err).Msg("health check: redis failed")
	} else {
		resp.Redis = "ok"
	}

	if err := h.checkMinIO(ctx); err != nil {
		resp.MinIO = "unhealthy: " + err.Error()
		allHealthy = false
		log.Error().Err(err).Msg("health check: minio failed")
	} else {
		resp.MinIO = "ok"
	}

	if !allHealthy {
		resp.Status = "degraded"
		c.JSON(http.StatusServiceUnavailable, resp)
		return
	}

	c.JSON(http.StatusOK, resp)
}