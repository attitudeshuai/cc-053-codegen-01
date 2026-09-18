package services

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"cc-053/internal/config"
	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type ExportService struct {
	cfg            *config.Config
	segmentRepo    *repository.SegmentRepo
	annotationRepo *repository.AnnotationRepo
	speakerRepo    *repository.SpeakerRepo
	wordlistRepo   *repository.WordlistRepo
	recordingRepo  *repository.RecordingRepo
	taskRepo       *repository.TaskRepo
	minioSvc       *MinIOService
}

func NewExportService(
	cfg *config.Config,
	segmentRepo *repository.SegmentRepo,
	annotationRepo *repository.AnnotationRepo,
	speakerRepo *repository.SpeakerRepo,
	wordlistRepo *repository.WordlistRepo,
	recordingRepo *repository.RecordingRepo,
	taskRepo *repository.TaskRepo,
	minioSvc *MinIOService,
) *ExportService {
	return &ExportService{
		cfg:            cfg,
		segmentRepo:    segmentRepo,
		annotationRepo: annotationRepo,
		speakerRepo:    speakerRepo,
		wordlistRepo:   wordlistRepo,
		recordingRepo:  recordingRepo,
		taskRepo:       taskRepo,
		minioSvc:       minioSvc,
	}
}

// GenerateExport builds the export zip package
func (s *ExportService) GenerateExport(ctx context.Context, jobID int64, filter models.CreateExportRequest) (string, error) {
	tmpDir, err := os.MkdirTemp("", "export-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subdirectories
	audioDir := filepath.Join(tmpDir, "audio")
	os.MkdirAll(audioDir, 0755)

	// Build metadata CSV
	metadataFile := filepath.Join(tmpDir, "metadata.csv")
	metadataRecords, err := s.buildMetadata(ctx, filter)
	if err != nil {
		return "", fmt.Errorf("failed to build metadata: %w", err)
	}
	if err := writeCSV(metadataFile, metadataRecords); err != nil {
		return "", fmt.Errorf("failed to write metadata.csv: %w", err)
	}

	// Build lexicon TSV
	lexiconFile := filepath.Join(tmpDir, "lexicon.tsv")
	lexiconRecords, err := s.buildLexicon(ctx, filter)
	if err != nil {
		return "", fmt.Errorf("failed to build lexicon: %w", err)
	}
	if err := writeTSV(lexiconFile, lexiconRecords); err != nil {
		return "", fmt.Errorf("failed to write lexicon.tsv: %w", err)
	}

	// Build README
	readmeFile := filepath.Join(tmpDir, "README.md")
	readmeContent := `# Dialect Corpus Export

## File Structure
- audio/          - WAV audio segments (16kHz, mono, 16-bit PCM)
- metadata.csv    - Segment-level metadata with annotations
- lexicon.tsv     - Word list with IPA transcriptions

## metadata.csv Fields
- segment_id: Unique segment identifier
- speaker_code: Anonymized speaker code
- entry_id: Word list entry ID
- hanzi: Chinese character
- gloss: Mandarin gloss
- ipa: IPA transcription (arbitrated)
- tone: Tone notation
- start_ms: Segment start time in ms
- end_ms: Segment end time in ms
- duration_ms: Segment duration in ms

## lexicon.tsv Fields
- entry_id: Entry ID
- hanzi: Chinese character
- gloss: Mandarin gloss
- ipa_ref: Reference IPA
- group: Semantic field group

## Notes
- Audio format: 16kHz, mono, 16-bit PCM WAV
- Compatible with ELAN, Praat, and standard analysis tools
`
	if err := os.WriteFile(readmeFile, []byte(readmeContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write README: %w", err)
	}

	// Create zip
	outputKey := fmt.Sprintf("exports/export_%d.zip", jobID)
	zipFile := filepath.Join(tmpDir, "export.zip")
	if err := createZip(zipFile, tmpDir); err != nil {
		return "", fmt.Errorf("failed to create zip: %w", err)
	}

	// Upload zip to MinIO (in production; for now return local path)
	// For MVP, we'll just return the local path as a placeholder
	log.Info().Str("output_key", outputKey).Str("zip_file", zipFile).Msg("export generated")

	return zipFile, nil
}

func (s *ExportService) buildMetadata(ctx context.Context, filter models.CreateExportRequest) ([][]string, error) {
	headers := []string{"segment_id", "speaker_code", "entry_id", "hanzi", "gloss", "ipa", "tone", "start_ms", "end_ms", "duration_ms"}
	records := [][]string{headers}

	query := models.SegmentQuery{
		TaskID: filter.TaskID,
		Status: filter.Status,
	}
	query.Limit = 10000

	segments, _, err := s.segmentRepo.List(query)
	if err != nil {
		return nil, err
	}

	for _, seg := range segments {
		annots, _ := s.annotationRepo.ListBySegment(seg.ID)

		// Find arbitrated annotation or first accepted one
		ipa := ""
		tone := ""
		for _, a := range annots {
			if a.Decision == "arbitrated" || a.Decision == "accept" {
				ipa = a.IPA
				tone = a.Tone
				break
			}
		}
		if ipa == "" && len(annots) > 0 {
			ipa = annots[0].IPA
			tone = annots[0].Tone
		}

		speakerCode := ""
		durationMs := seg.EndMs - seg.StartMs

		records = append(records, []string{
			fmt.Sprintf("%d", seg.ID),
			speakerCode,
			fmt.Sprintf("%d", seg.EntryID),
			"", // hanzi from wordlist
			"", // gloss
			ipa,
			tone,
			fmt.Sprintf("%d", seg.StartMs),
			fmt.Sprintf("%d", seg.EndMs),
			fmt.Sprintf("%d", durationMs),
		})
	}

	return records, nil
}

func (s *ExportService) buildLexicon(ctx context.Context, filter models.CreateExportRequest) ([][]string, error) {
	headers := []string{"entry_id", "hanzi", "gloss", "ipa_ref", "group"}
	records := [][]string{headers}

	if filter.WordlistID > 0 {
		wordlist, err := s.wordlistRepo.GetByID(filter.WordlistID)
		if err == nil {
			for _, entry := range wordlist.Entries {
				records = append(records, []string{
					fmt.Sprintf("%d", entry.ID),
					entry.Hanzi,
					entry.Gloss,
					entry.IPARef,
					entry.Group,
				})
			}
		}
	}

	return records, nil
}

func writeCSV(path string, records [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write UTF-8 BOM for Excel compatibility
	f.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(f)
	return w.WriteAll(records)
}

func writeTSV(path string, records [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, record := range records {
		line := strings.Join(record, "\t") + "\n"
		if _, err := io.WriteString(f, line); err != nil {
			return err
		}
	}
	return nil
}

func createZip(zipPath, sourceDir string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == sourceDir {
			return nil
		}

		relPath, _ := filepath.Rel(sourceDir, path)
		if relPath == "export.zip" {
			return nil
		}

		if info.IsDir() {
			_, err := zw.Create(relPath + "/")
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = relPath
		header.Method = zip.Deflate

		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		_, err = io.Copy(w, src)
		return err
	})
}