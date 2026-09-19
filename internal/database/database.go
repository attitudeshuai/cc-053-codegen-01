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

		// ---- 002 方言调查点档案 ----
		`CREATE TABLE IF NOT EXISTS counties (
			id         BIGSERIAL PRIMARY KEY,
			name       VARCHAR(100) NOT NULL,
			code       VARCHAR(50)  NOT NULL UNIQUE,
			note       VARCHAR(500) DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS dialect_regions (
			id         BIGSERIAL PRIMARY KEY,
			name       VARCHAR(100) NOT NULL,
			code       VARCHAR(50)  NOT NULL UNIQUE,
			level      INT NOT NULL CHECK (level >= 1),
			parent_id  BIGINT REFERENCES dialect_regions(id),
			path       BIGINT[] NOT NULL DEFAULT '{}',
			note       VARCHAR(500) DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT regions_leaf_check CHECK (parent_id IS NOT NULL OR level = 1)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_regions_parent ON dialect_regions(parent_id)`,
		`CREATE TABLE IF NOT EXISTS survey_points (
			id         BIGSERIAL PRIMARY KEY,
			name       VARCHAR(100) NOT NULL,
			code       VARCHAR(50)  NOT NULL UNIQUE,
			county_id  BIGINT NOT NULL REFERENCES counties(id),
			status     VARCHAR(20) NOT NULL DEFAULT 'active'
			           CHECK (status IN ('active','merged')),
			merged_into_id BIGINT REFERENCES survey_points(id),
			note       VARCHAR(500) DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_points_county ON survey_points(county_id)`,
		`CREATE INDEX IF NOT EXISTS idx_points_merged_into ON survey_points(merged_into_id)`,
		`CREATE TABLE IF NOT EXISTS survey_point_aliases (
			id           BIGSERIAL PRIMARY KEY,
			point_id     BIGINT NOT NULL REFERENCES survey_points(id) ON DELETE CASCADE,
			name         VARCHAR(100) NOT NULL,
			fingerprint  VARCHAR(100) NOT NULL,
			source       VARCHAR(20) NOT NULL DEFAULT 'manual'
			             CHECK (source IN ('manual','canonical','merge'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_aliases_fingerprint ON survey_point_aliases(fingerprint)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_aliases_point_name ON survey_point_aliases(point_id, fingerprint)`,
		`CREATE INDEX IF NOT EXISTS idx_aliases_point ON survey_point_aliases(point_id)`,
		`CREATE TABLE IF NOT EXISTS point_assignments (
			id              BIGSERIAL PRIMARY KEY,
			point_id        BIGINT NOT NULL REFERENCES survey_points(id),
			region_id       BIGINT NOT NULL REFERENCES dialect_regions(id),
			effective_date  DATE NOT NULL,
			end_date        DATE,
			created_by      VARCHAR(200) NOT NULL DEFAULT '',
			note            VARCHAR(500) DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_assignments_current
			ON point_assignments(point_id) WHERE (end_date IS NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_assignments_eff
			ON point_assignments(point_id, effective_date)`,
		`CREATE INDEX IF NOT EXISTS idx_assignments_region ON point_assignments(region_id)`,
		`CREATE TABLE IF NOT EXISTS point_records (
			id               BIGSERIAL PRIMARY KEY,
			point_id         BIGINT NOT NULL REFERENCES survey_points(id),
			content          TEXT NOT NULL,
			record_date      DATE NOT NULL,
			snapshot_county  VARCHAR(100) NOT NULL,
			snapshot_region  VARCHAR(100) NOT NULL,
			snapshot_path    TEXT NOT NULL DEFAULT '',
			created_by       VARCHAR(200) NOT NULL DEFAULT '',
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_records_point ON point_records(point_id)`,
		`CREATE INDEX IF NOT EXISTS idx_records_date ON point_records(record_date)`,
		`CREATE TABLE IF NOT EXISTS point_merges (
			id                 BIGSERIAL PRIMARY KEY,
			surviving_point_id BIGINT NOT NULL REFERENCES survey_points(id),
			merged_point_id    BIGINT NOT NULL REFERENCES survey_points(id),
			surviving_before   INT NOT NULL,
			merged_before      INT NOT NULL,
			total_after        INT NOT NULL,
			merged_by          VARCHAR(200) NOT NULL DEFAULT '',
			note               VARCHAR(500) DEFAULT '',
			created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE OR REPLACE FUNCTION forbid_snapshot_mutation() RETURNS trigger AS $$
		BEGIN
			-- 归属快照字段一个都不许改；只有合并时转移 point_id 放行
			IF NEW.snapshot_county IS DISTINCT FROM OLD.snapshot_county
			    OR NEW.snapshot_region IS DISTINCT FROM OLD.snapshot_region
			    OR NEW.snapshot_path   IS DISTINCT FROM OLD.snapshot_path
			    OR NEW.record_date     IS DISTINCT FROM OLD.record_date
			    OR NEW.content         IS DISTINCT FROM OLD.content THEN
				RAISE EXCEPTION 'point_records 归属快照不可改（id=%）', OLD.id
				    USING ERRCODE = 'check_violation';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`,
		`DROP TRIGGER IF EXISTS trg_records_no_update ON point_records`,
		`CREATE TRIGGER trg_records_no_update BEFORE UPDATE ON point_records
			FOR EACH ROW EXECUTE FUNCTION forbid_snapshot_mutation()`,
		`DROP TRIGGER IF EXISTS trg_records_no_delete ON point_records`,
		`CREATE TRIGGER trg_records_no_delete BEFORE DELETE ON point_records
			FOR EACH ROW EXECUTE FUNCTION forbid_snapshot_mutation()`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}

	log.Info().Msg("database migrations completed")
	return nil
}