package models

type CreateWordlistRequest struct {
	Name    string  `json:"name" binding:"required"`
	Entries []Entry `json:"entries" binding:"required"`
}

type CreateSpeakerRequest struct {
	CodeName         string `json:"code_name" binding:"required"`
	BirthYear        int    `json:"birth_year" binding:"required"`
	Gender           string `json:"gender" binding:"required,oneof=male female other"`
	DialectPointCode string `json:"dialect_point_code" binding:"required"`
	Occupation       string `json:"occupation"`
	YearsAway        int    `json:"years_away"`
	ContactRef       string `json:"contact_ref"`
}

type CreateTaskRequest struct {
	WordlistID int64  `json:"wordlist_id" binding:"required"`
	SpeakerID  int64  `json:"speaker_id" binding:"required"`
	Kind       string `json:"kind" binding:"required,oneof=record annotate"`
	Assignee   string `json:"assignee"`
}

type UploadURLRequest struct {
	Filename string `json:"filename" binding:"required"`
	TaskID   int64  `json:"task_id" binding:"required"`
}

type UploadURLResponse struct {
	URL        string `json:"url"`
	ObjectKey  string `json:"object_key"`
	ExpiresIn  int    `json:"expires_in"`
}

type CreateRecordingRequest struct {
	TaskID     int64  `json:"task_id" binding:"required"`
	ObjectKey  string `json:"object_key" binding:"required"`
	DurationMs int    `json:"duration_ms"`
	SampleRate int    `json:"sample_rate"`
	PeakDB     float64 `json:"peak_db"`
	Device     string `json:"device"`
	RecordedAt string `json:"recorded_at"`
}

type AnnotationRequest struct {
	IPA       string `json:"ipa" binding:"required"`
	Tone      string `json:"tone"`
	Note      string `json:"note"`
	Version   int    `json:"version" binding:"required"`
}

type ArbitrateRequest struct {
	WinnerAnnotationID int64  `json:"winner_annotation_id" binding:"required"`
	Arbiter            string `json:"arbiter" binding:"required"`
	Reason             string `json:"reason"`
}

type CreateExportRequest struct {
	WordlistID int64 `json:"wordlist_id"`
	TaskID     int64 `json:"task_id"`
	SpeakerID  int64 `json:"speaker_id"`
	Status     string `json:"status"`
}

type SegmentQuery struct {
	TaskID int64  `form:"task_id"`
	Status string `form:"status"`
	Pagination
}

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Database  string `json:"database"`
	Redis     string `json:"redis"`
	MinIO     string `json:"minio"`
}