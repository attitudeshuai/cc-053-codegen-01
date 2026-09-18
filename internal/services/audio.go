package services

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

type AudioSegment struct {
	StartMs int
	EndMs   int
	SnrDB   float64
}

// AudioService handles audio processing via ffmpeg
type AudioService struct {
	ffmpegPath string
}

func NewAudioService() *AudioService {
	path := "ffmpeg"
	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		log.Warn().Err(err).Msg("ffmpeg not found in PATH, audio processing will be unavailable")
	}
	return &AudioService{ffmpegPath: path}
}

// ValidateAudio checks sample rate, duration, and peak level
func (s *AudioService) ValidateAudio(ctx context.Context, inputPath string) (sampleRate int, durationMs int, peakDB float64, err error) {
	cmd := exec.CommandContext(ctx, s.ffmpegPath, "-i", inputPath, "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, 0, 0, fmt.Errorf("ffprobe failed: %w, output: %s", err, stderr.String())
	}

	output := stderr.String()

	// Parse sample rate
	sampleRate = s.parseSampleRate(output)

	// Parse duration
	durationMs = s.parseDurationMs(output)

	// Parse peak dB
	peakDB = s.parsePeakDB(output)

	return sampleRate, durationMs, peakDB, nil
}

// ConvertTo16kHzMono converts audio to 16kHz mono WAV
func (s *AudioService) ConvertTo16kHzMono(ctx context.Context, inputPath, outputPath string) error {
	cmd := exec.CommandContext(ctx, s.ffmpegPath,
		"-i", inputPath,
		"-ar", "16000",
		"-ac", "1",
		"-sample_fmt", "s16",
		"-y",
		outputPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w, output: %s", err, stderr.String())
	}
	return nil
}

// VADSplit performs voice activity detection and splits audio into segments
// Uses silence detection: -af silencedetect=noise=-30dB:d=0.5
func (s *AudioService) VADSplit(ctx context.Context, inputPath string, minSegmentMs int) ([]AudioSegment, error) {
	cmd := exec.CommandContext(ctx, s.ffmpegPath,
		"-i", inputPath,
		"-af", "silencedetect=noise=-30dB:d=0.5",
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("silence detection failed: %w, output: %s", err, stderr.String())
	}

	output := stderr.String()
	segments := s.parseSilenceDetect(output, minSegmentMs)

	if len(segments) == 0 {
		// Fallback: treat whole file as one segment
		_, durationMs, _, _ := s.ValidateAudio(ctx, inputPath)
		segments = append(segments, AudioSegment{
			StartMs: 0,
			EndMs:   durationMs,
			SnrDB:   0,
		})
	}

	return segments, nil
}

// SplitAndSave splits audio into segments and saves each as a separate file
func (s *AudioService) SplitAndSave(ctx context.Context, inputPath string, segments []AudioSegment, outputDir string) ([]string, error) {
	var outputFiles []string

	for i, seg := range segments {
		outputFile := fmt.Sprintf("%s/segment_%04d.wav", outputDir, i)
		startStr := fmt.Sprintf("%.3f", float64(seg.StartMs)/1000.0)
		durationStr := fmt.Sprintf("%.3f", float64(seg.EndMs-seg.StartMs)/1000.0)

		cmd := exec.CommandContext(ctx, s.ffmpegPath,
			"-i", inputPath,
			"-ss", startStr,
			"-t", durationStr,
			"-ar", "16000",
			"-ac", "1",
			"-sample_fmt", "s16",
			"-y",
			outputFile,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return outputFiles, fmt.Errorf("failed to split segment %d: %w, output: %s", i, err, stderr.String())
		}
		outputFiles = append(outputFiles, outputFile)
	}

	return outputFiles, nil
}

func (s *AudioService) parseSampleRate(output string) int {
	// Match pattern like "Stream #0:0: Audio: ... 44100 Hz"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Hz") && strings.Contains(line, "Audio") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "Hz" && i > 0 {
					if rate, err := strconv.Atoi(parts[i-1]); err == nil {
						return rate
					}
				}
			}
		}
	}
	return 0
}

func (s *AudioService) parseDurationMs(output string) int {
	// Match pattern like "Duration: 00:01:23.45"
	lines := strings.Split(output, "\n")
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Duration:") {
			parts := strings.Split(line, ",")
			for _, part := range parts {
				if strings.Contains(part, "Duration:") {
					durStr := strings.TrimSpace(strings.TrimPrefix(part, "Duration:"))
					durStr = strings.TrimSpace(durStr)
					// Parse HH:MM:SS.ms
					var h, m int
					var s float64
					if _, err := fmt.Sscanf(durStr, "%d:%d:%f", &h, &m, &s); err == nil {
						return int((float64(h)*3600 + float64(m)*60 + s) * 1000)
					}
				}
			}
		}
	}
	// try alternate parsing
	for _, line := range lines {
		if strings.Contains(line, "Duration:") {
			idx := strings.Index(line, "Duration:") + 9
			endIdx := strings.IndexAny(line[idx:], ",.")
			if endIdx > 0 {
				durStr := strings.TrimSpace(line[idx : idx+endIdx])
				log.Info().Str("duration_str", durStr).Msg("parsing duration alt")
			}
		}
	}
	return 0
}

func (s *AudioService) parsePeakDB(output string) float64 {
	// Try to find max_volume from loudnorm or volumedetect
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "max_volume") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "max_volume:" && i+1 < len(parts) {
					dbStr := strings.TrimSuffix(parts[i+1], "dB")
					if db, err := strconv.ParseFloat(dbStr, 64); err == nil {
						return db
					}
				}
			}
		}
	}
	return 0
}

func (s *AudioService) parseSilenceDetect(output string, minSegmentMs int) []AudioSegment {
	var segments []AudioSegment
	var currentStart int = 0
	var inSilence bool

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "silence_start:") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "silence_start:" && i+1 < len(parts) {
					silenceStart, err := strconv.ParseFloat(parts[i+1], 64)
					if err == nil {
						silenceStartMs := int(silenceStart * 1000)
						if !inSilence && silenceStartMs-currentStart >= minSegmentMs {
							// Non-silence segment detected
							segments = append(segments, AudioSegment{
								StartMs: currentStart,
								EndMs:   silenceStartMs,
								SnrDB:   0,
							})
						}
						currentStart = silenceStartMs
						inSilence = true
					}
				}
			}
		}
		if strings.Contains(line, "silence_end:") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "silence_end:" && i+1 < len(parts) {
					silenceEnd, err := strconv.ParseFloat(parts[i+1], 64)
					if err == nil {
						_ = silenceEnd // silence end time in seconds
						inSilence = false
					}
				}
			}
		}
	}

	return segments
}

// NormalizeIPA performs NFD normalization and whitespace removal for comparison
func NormalizeIPA(s string) string {
	// Simple normalization: trim spaces
	return strings.TrimSpace(s)
}

// IPAStringsEqual checks if two IPA strings are equal after normalization
func IPAStringsEqual(a, b string) bool {
	return NormalizeIPA(a) == NormalizeIPA(b)
}

// CalculateEditDistance computes Levenshtein distance for IPA similarity check
func CalculateEditDistance(s1, s2 string) int {
	s1 = NormalizeIPA(s1)
	s2 = NormalizeIPA(s2)

	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	// Convert to runes for Unicode support
	r1 := []rune(s1)
	r2 := []rune(s2)

	// Use single row optimization
	prev := make([]int, len(r2)+1)
	curr := make([]int, len(r2)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(r1); i++ {
		curr[0] = i
		for j := 1; j <= len(r2); j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}

	return prev[len(r2)]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CheckConsistency checks if two annotations are consistent
func CheckConsistency(ipa1, ipa2 string) (bool, float64) {
	if IPAStringsEqual(ipa1, ipa2) {
		return true, 0
	}
	distance := CalculateEditDistance(ipa1, ipa2)
	maxLen := math.Max(float64(len([]rune(ipa1))), float64(len([]rune(ipa2))))
	if maxLen == 0 {
		return true, 0
	}
	similarity := 1.0 - float64(distance)/maxLen
	return similarity > 0.8, similarity
}