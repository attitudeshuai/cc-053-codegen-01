package worker

const (
	TypeProcessRecording = "process:recording"
	TypeTranscode        = "process:transcode"
	TypeVADSplit         = "process:vad_split"
	TypeReindexSegment   = "process:reindex"
)

type ProcessRecordingPayload struct {
	RecordingID int64  `json:"recording_id"`
	ObjectKey   string `json:"object_key"`
}

type TranscodePayload struct {
	RecordingID int64  `json:"recording_id"`
	ObjectKey   string `json:"object_key"`
}

type VADSplitPayload struct {
	RecordingID int64  `json:"recording_id"`
	ObjectKey   string `json:"object_key"`
	TempDir     string `json:"temp_dir"`
}

type ReindexPayload struct {
	RecordingID int64 `json:"recording_id"`
}