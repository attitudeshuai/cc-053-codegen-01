package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"cc-053/internal/config"
	"cc-053/internal/database"
	"cc-053/internal/handlers"
	"cc-053/internal/middleware"
	"cc-053/internal/repository"
	"cc-053/internal/services"
)

func main() {
	// Load config
	cfg := config.Load()

	// Initialize logger
	log.Info().Str("port", cfg.ServerPort).Msg("starting API server")

	// Docker HEALTHCHECK 探针：运行层无 shell，由二进制自查 /healthz
	if len(os.Args) > 1 && os.Args[1] == "health" {
		resp, err := http.Get("http://localhost:" + cfg.ServerPort + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		resp.Body.Close()
		os.Exit(0)
	}

	// Connect to database
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Close()

	// Run migrations
	if err := database.RunMigrations(db); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}

	// Initialize repositories
	speakerRepo := repository.NewSpeakerRepo(db)
	wordlistRepo := repository.NewWordlistRepo(db)
	taskRepo := repository.NewTaskRepo(db)
	recordingRepo := repository.NewRecordingRepo(db)
	segmentRepo := repository.NewSegmentRepo(db)
	annotationRepo := repository.NewAnnotationRepo(db)
	arbitrationRepo := repository.NewArbitrationRepo(db)
	exportRepo := repository.NewExportRepo(db)

	// 调查点档案
	countyRepo := repository.NewCountyRepo(db)
	regionRepo := repository.NewRegionRepo(db)
	surveyPointRepo := repository.NewSurveyPointRepo(db, regionRepo, countyRepo)

	// Initialize MinIO service
	minioSvc, err := services.NewMinIOService(cfg)
	if err != nil {
		log.Warn().Err(err).Msg("MinIO not available, running without object storage")
		minioSvc = nil
	}

	// Initialize export service
	exportSvc := services.NewExportService(cfg, segmentRepo, annotationRepo, speakerRepo, wordlistRepo, recordingRepo, taskRepo, minioSvc)

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(
		func(ctx context.Context) error { return db.PingContext(ctx) },
		func(ctx context.Context) error {
			// Redis health check - skip if not configured
			return nil
		},
		func(ctx context.Context) error {
			if minioSvc == nil {
				return nil
			}
			return minioSvc.HealthCheck(ctx)
		},
	)

	speakerHandler := handlers.NewSpeakerHandler(speakerRepo)
	wordlistHandler := handlers.NewWordlistHandler(wordlistRepo)
	taskHandler := handlers.NewTaskHandler(taskRepo)
	recordingHandler := handlers.NewRecordingHandler(recordingRepo, taskRepo, minioSvc)
	segmentHandler := handlers.NewSegmentHandler(segmentRepo)
	annotationHandler := handlers.NewAnnotationHandler(annotationRepo, segmentRepo)
	arbitrationHandler := handlers.NewArbitrationHandler(arbitrationRepo, annotationRepo, segmentRepo)
	exportHandler := handlers.NewExportHandler(exportRepo, exportSvc)

	countyHandler := handlers.NewCountyHandler(countyRepo)
	regionHandler := handlers.NewRegionHandler(regionRepo)
	surveyPointHandler := handlers.NewSurveyPointHandler(surveyPointRepo)

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.CORSMiddleware())

	// Health check
	r.GET("/healthz", healthHandler.Health)

	// API v1 routes
	v1 := r.Group("/api/v1")
	{
		v1.POST("/speakers", speakerHandler.Create)
		v1.GET("/speakers/:id", speakerHandler.GetByID)
		v1.GET("/speakers", speakerHandler.List)

		v1.POST("/wordlists", wordlistHandler.Create)
		v1.GET("/wordlists/:id", wordlistHandler.GetByID)
		v1.GET("/wordlists", wordlistHandler.List)

		v1.POST("/tasks", taskHandler.Create)
		v1.GET("/tasks/:id", taskHandler.GetByID)
		v1.GET("/tasks", taskHandler.List)

		v1.POST("/recordings/upload-url", recordingHandler.GetUploadURL)
		v1.POST("/recordings", recordingHandler.Create)
		v1.GET("/recordings/:id", recordingHandler.GetByID)

		v1.GET("/segments", segmentHandler.List)

		v1.PUT("/segments/:id/annotation", annotationHandler.Submit)
		v1.POST("/segments/:id/arbitrate", arbitrationHandler.Arbitrate)

		v1.POST("/exports", exportHandler.Create)
		v1.GET("/exports/:id", exportHandler.GetByID)
		v1.GET("/exports", exportHandler.List)

		// 方言调查点档案
		v1.POST("/counties", countyHandler.Create)
		v1.GET("/counties/:id", countyHandler.GetByID)
		v1.GET("/counties", countyHandler.List)

		v1.POST("/dialect-regions", regionHandler.Create)
		v1.GET("/dialect-regions/:id", regionHandler.GetByID)
		v1.GET("/dialect-regions", regionHandler.List)
		v1.GET("/dialect-regions/tree", regionHandler.Tree)

		v1.POST("/survey-points", surveyPointHandler.Create)
		v1.GET("/survey-points", surveyPointHandler.List)
		v1.POST("/survey-points/merge", surveyPointHandler.Merge)
		v1.GET("/survey-points/merges", surveyPointHandler.MergeHistory)
		v1.GET("/survey-points/:id", surveyPointHandler.GetByID)
		v1.GET("/survey-points/:id/history", surveyPointHandler.History)
		v1.GET("/survey-points/:id/assignments", surveyPointHandler.Assignments)
		v1.GET("/survey-points/:id/assignment-at", surveyPointHandler.AssignmentAt)
		v1.PUT("/survey-points/:id/assignment", surveyPointHandler.Adjust)
		v1.POST("/survey-points/:id/records", surveyPointHandler.CreateRecord)
	}

	// Create HTTP server with proper timeouts for large file uploads
	srv := &http.Server{
		Addr:         cfg.ServerHost + ":" + cfg.ServerPort,
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Info().Str("addr", srv.Addr).Msg("server listening")
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}