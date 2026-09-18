package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"

	"cc-053/internal/config"
)

func Connect(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info().Msg("database connected successfully")
	return db, nil
}

func RunMigrations(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS speakers (
			id BIGSERIAL PRIMARY KEY,
			code_name VARCHAR(100) NOT NULL UNIQUE,
			birth_year INT NOT NULL,
			gender VARCHAR(10) NOT NULL,
			dialect_point_code VARCHAR(50) NOT NULL,
			occupation VARCHAR(200) DEFAULT '',
			years_away INT DEFAULT 0,
			contact_ref VARCHAR(255) DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS wordlists (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(200) NOT NULL,
			version INT NOT NULL DEFAULT 1,
			entries JSONB NOT NULL DEFAULT '[]',
			is_current BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id BIGSERIAL PRIMARY KEY,
			wordlist_id BIGINT NOT NULL REFERENCES wordlists(id),
			speaker_id BIGINT NOT NULL REFERENCES speakers(id),
			kind VARCHAR(20) NOT NULL CHECK (kind IN ('record','annotate')),
			assignee VARCHAR(200) DEFAULT '',
			status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','in_progress','completed','failed')),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS recordings (
			id BIGSERIAL PRIMARY KEY,
			task_id BIGINT NOT NULL REFERENCES tasks(id),
			object_key VARCHAR(500) NOT NULL,
			duration_ms INT NOT NULL DEFAULT 0,
			sample_rate INT NOT NULL DEFAULT 0,
			peak_db DECIMAL(6,2) DEFAULT 0,
			device VARCHAR(200) DEFAULT '',
			recorded_at TIMESTAMPTZ,
			status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','rejected')),
			reject_reason TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS segments (
			id BIGSERIAL PRIMARY KEY,
			recording_id BIGINT NOT NULL REFERENCES recordings(id),
			entry_id BIGINT NOT NULL,
			start_ms INT NOT NULL,
			end_ms INT NOT NULL,
			object_key VARCHAR(500) NOT NULL DEFAULT '',
			snr_db DECIMAL(6,2) DEFAULT 0,
			status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','annotated','in_arbitration','completed','failed')),
			version INT NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS annotations (
			id BIGSERIAL PRIMARY KEY,
			segment_id BIGINT NOT NULL REFERENCES segments(id),
			annotator VARCHAR(200) NOT NULL,
			ipa TEXT DEFAULT '',
			tone VARCHAR(50) DEFAULT '',
			note TEXT DEFAULT '',
			decision VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending','accept','reject','arbitrated')),
			version INT NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS arbitrations (
			id BIGSERIAL PRIMARY KEY,
			segment_id BIGINT NOT NULL REFERENCES segments(id),
			winner_annotation_id BIGINT NOT NULL REFERENCES annotations(id),
			arbiter VARCHAR(200) NOT NULL,
			reason TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS export_jobs (
			id BIGSERIAL PRIMARY KEY,
			filter JSONB NOT NULL DEFAULT '{}',
			status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','failed')),
			progress INT DEFAULT 0,
			output_key VARCHAR(500) DEFAULT '',
			error_message TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS annotation_history (
			id BIGSERIAL PRIMARY KEY,
			segment_id BIGINT NOT NULL REFERENCES segments(id),
			annotation_id BIGINT REFERENCES annotations(id),
			old_ipa TEXT DEFAULT '',
			old_tone VARCHAR(50) DEFAULT '',
			new_ipa TEXT DEFAULT '',
			new_tone VARCHAR(50) DEFAULT '',
			changed_by VARCHAR(200) NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_wordlist ON tasks(wordlist_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_speaker ON tasks(speaker_id)`,
		`CREATE INDEX IF NOT EXISTS idx_recordings_task ON recordings(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_segments_recording ON segments(recording_id)`,
		`CREATE INDEX IF NOT EXISTS idx_segments_status ON segments(status)`,
		`CREATE INDEX IF NOT EXISTS idx_annotations_segment ON annotations(segment_id)`,
		`CREATE INDEX IF NOT EXISTS idx_arbitrations_segment ON arbitrations(segment_id)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}

	log.Info().Msg("database migrations completed")
	return nil
}