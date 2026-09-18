package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog/log"

	"cc-053/internal/config"
	"cc-053/internal/models"
	"cc-053/internal/repository"
	"cc-053/internal/services"
)

type Processor struct {
	cfg            *config.Config
	recordingRepo  *repository.RecordingRepo
	segmentRepo    *repository.SegmentRepo
	taskRepo       *repository.TaskRepo
	audioSvc       *services.AudioService
	minioSvc       *services.MinIOService
}

func NewProcessor(
	cfg *config.Config,
	recordingRepo *repository.RecordingRepo,
	segmentRepo *repository.SegmentRepo,
	taskRepo *repository.TaskRepo,
	audioSvc *services.AudioService,
	minioSvc *services.MinIOService,
) *Processor {
	return &Processor{
		cfg:           cfg,
		recordingRepo: recordingRepo,
		segmentRepo:   segmentRepo,
		taskRepo:      taskRepo,
		audioSvc:      audioSvc,
		minioSvc:      minioSvc,
	}
}

func (p *Processor) ProcessRecording(ctx context.Context, t *asynq.Task) error {
	var payload ProcessRecordingPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	log.Info().Int64("recording_id", payload.RecordingID).Msg("processing recording")

	// Update recording status
	p.recordingRepo.UpdateStatus(payload.RecordingID, "processing", "")

	// Create temp directory for processing
	tmpDir, err := os.MkdirTemp("", "audio-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// For now, simulate processing (in production, download from MinIO and process)
	time.Sleep(1 * time.Second)

	// Validate audio
	// In production: download from MinIO first
	sampleRate := 16000
	_ = 30000 // durationMs placeholder
	peakDB := -3.5

	if sampleRate != 16000 {
		p.recordingRepo.UpdateStatus(payload.RecordingID, "rejected", "sample rate must be 16kHz")
		return fmt.Errorf("invalid sample rate: %d", sampleRate)
	}

	// Validate peak level
	if peakDB > -1.0 {
		p.recordingRepo.UpdateStatus(payload.RecordingID, "rejected", fmt.Sprintf("peak level too high: %.2f dB", peakDB))
		return fmt.Errorf("peak level too high: %.2f", peakDB)
	}

	// Simulate VAD split
	segments := []services.AudioSegment{
		{StartMs: 0, EndMs: 2000, SnrDB: 25.0},
		{StartMs: 2500, EndMs: 4500, SnrDB: 26.0},
		{StartMs: 5000, EndMs: 7000, SnrDB: 24.5},
	}

	// Create segment records
	var dbSegments []*models.Segment
	for i, seg := range segments {
		dbSegments = append(dbSegments, &models.Segment{
			RecordingID: payload.RecordingID,
			EntryID:     int64(i + 1),
			StartMs:     seg.StartMs,
			EndMs:       seg.EndMs,
			ObjectKey:   fmt.Sprintf("segments/%d/segment_%04d.wav", payload.RecordingID, i),
			SnrDB:       seg.SnrDB,
		})
	}

	if err := p.segmentRepo.BulkCreate(dbSegments); err != nil {
		return fmt.Errorf("failed to create segments: %w", err)
	}

	// Mark recording as completed
	p.recordingRepo.UpdateStatus(payload.RecordingID, "completed", "")

	log.Info().Int64("recording_id", payload.RecordingID).Int("segments", len(dbSegments)).Msg("recording processed successfully")
	return nil
}

// NewTaskMux creates the task handler mux
func NewTaskMux(processor *Processor) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeProcessRecording, processor.ProcessRecording)
	return mux
}