package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog/log"

	"cc-053/internal/config"
	"cc-053/internal/database"
	"cc-053/internal/repository"
	"cc-053/internal/services"
	"cc-053/internal/worker"
)

func main() {
	cfg := config.Load()

	log.Info().Int("concurrency", cfg.AsynqConcurrency).Msg("starting worker")

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
	recordingRepo := repository.NewRecordingRepo(db)
	segmentRepo := repository.NewSegmentRepo(db)
	taskRepo := repository.NewTaskRepo(db)

	// Initialize services
	audioSvc := services.NewAudioService()

	minioSvc, err := services.NewMinIOService(cfg)
	if err != nil {
		log.Warn().Err(err).Msg("MinIO not available, running without object storage")
		minioSvc = nil
	}

	// Initialize processor
	processor := worker.NewProcessor(cfg, recordingRepo, segmentRepo, taskRepo, audioSvc, minioSvc)

	// Create asynq redis connection
	redisOpt := asynq.RedisClientOpt{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	// Create and start worker server
	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: cfg.AsynqConcurrency,
		Queues: map[string]int{
			"critical": 6,
			"default":  3,
			"low":      1,
		},
	})

	mux := worker.NewTaskMux(processor)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Info().Msg("shutting down worker...")
		srv.Shutdown()
	}()

	log.Info().Msg("worker started, waiting for tasks...")
	if err := srv.Run(mux); err != nil {
		log.Fatal().Err(err).Msg("worker server failed")
	}
}