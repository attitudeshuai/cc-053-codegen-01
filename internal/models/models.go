package models

import (
	"time"
)

type Speaker struct {
	ID               int64     `json:"id"`
	CodeName         string    `json:"code_name"`
	BirthYear        int       `json:"birth_year"`
	Gender           string    `json:"gender"`
	DialectPointCode string    `json:"dialect_point_code"`
	Occupation       string    `json:"occupation"`
	YearsAway        int       `json:"years_away"`
	ContactRef       string    `json:"contact_ref,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Wordlist struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Version   int       `json:"version"`
	Entries   []Entry   `json:"entries"`
	IsCurrent bool      `json:"is_current"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Entry struct {
	ID     int64  `json:"id"`
	Hanzi  string `json:"hanzi"`
	Gloss  string `json:"gloss"`
	IPARef string `json:"ipa_ref,omitempty"`
	Group  string `json:"group,omitempty"`
}

type Task struct {
	ID         int64     `json:"id"`
	WordlistID int64     `json:"wordlist_id"`
	SpeakerID  int64     `json:"speaker_id"`
	Kind       string    `json:"kind"` // record | annotate
	Assignee   string    `json:"assignee"`
	Status     string    `json:"status"` // pending | in_progress | completed | failed
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Recording struct {
	ID           int64      `json:"id"`
	TaskID       int64      `json:"task_id"`
	ObjectKey    string     `json:"object_key"`
	DurationMs   int        `json:"duration_ms"`
	SampleRate   int        `json:"sample_rate"`
	PeakDB       float64    `json:"peak_db"`
	Device       string     `json:"device"`
	RecordedAt   *time.Time `json:"recorded_at"`
	Status       string     `json:"status"` // pending | processing | completed | rejected
	RejectReason string     `json:"reject_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type Segment struct {
	ID          int64     `json:"id"`
	RecordingID int64     `json:"recording_id"`
	EntryID     int64     `json:"entry_id"`
	StartMs     int       `json:"start_ms"`
	EndMs       int       `json:"end_ms"`
	ObjectKey   string    `json:"object_key"`
	SnrDB       float64   `json:"snr_db"`
	Status      string    `json:"status"` // pending | annotated | in_arbitration | completed | failed
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Annotation struct {
	ID        int64     `json:"id"`
	SegmentID int64     `json:"segment_id"`
	Annotator string    `json:"annotator"`
	IPA       string    `json:"ipa"`
	Tone      string    `json:"tone"`
	Note      string    `json:"note"`
	Decision  string    `json:"decision"` // pending | accept | reject | arbitrated
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Arbitration struct {
	ID                 int64     `json:"id"`
	SegmentID          int64     `json:"segment_id"`
	WinnerAnnotationID int64     `json:"winner_annotation_id"`
	Arbiter            string    `json:"arbiter"`
	Reason             string    `json:"reason"`
	CreatedAt          time.Time `json:"created_at"`
}

type ExportJob struct {
	ID          int64     `json:"id"`
	Filter      string    `json:"filter"`
	Status      string    `json:"status"` // pending | processing | completed | failed
	Progress    int       `json:"progress"`
	OutputKey   string    `json:"output_key"`
	ErrorMessage string   `json:"error_message,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Pagination
type Pagination struct {
	Offset int `form:"offset" json:"offset"`
	Limit  int `form:"limit" json:"limit"`
}

func (p *Pagination) Normalize() {
	if p.Limit <= 0 || p.Limit > 100 {
		p.Limit = 20
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
}