-- Dialect Corpus Platform - Initial Schema
-- This file is for reference; actual migrations run via internal/database/database.go

CREATE TABLE IF NOT EXISTS speakers (
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
);

CREATE TABLE IF NOT EXISTS wordlists (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    version INT NOT NULL DEFAULT 1,
    entries JSONB NOT NULL DEFAULT '[]',
    is_current BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tasks (
    id BIGSERIAL PRIMARY KEY,
    wordlist_id BIGINT NOT NULL REFERENCES wordlists(id),
    speaker_id BIGINT NOT NULL REFERENCES speakers(id),
    kind VARCHAR(20) NOT NULL CHECK (kind IN ('record','annotate')),
    assignee VARCHAR(200) DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','in_progress','completed','failed')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS recordings (
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
);

CREATE TABLE IF NOT EXISTS segments (
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
);

CREATE TABLE IF NOT EXISTS annotations (
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
);

CREATE TABLE IF NOT EXISTS arbitrations (
    id BIGSERIAL PRIMARY KEY,
    segment_id BIGINT NOT NULL REFERENCES segments(id),
    winner_annotation_id BIGINT NOT NULL REFERENCES annotations(id),
    arbiter VARCHAR(200) NOT NULL,
    reason TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS export_jobs (
    id BIGSERIAL PRIMARY KEY,
    filter JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','failed')),
    progress INT DEFAULT 0,
    output_key VARCHAR(500) DEFAULT '',
    error_message TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS annotation_history (
    id BIGSERIAL PRIMARY KEY,
    segment_id BIGINT NOT NULL REFERENCES segments(id),
    annotation_id BIGINT REFERENCES annotations(id),
    old_ipa TEXT DEFAULT '',
    old_tone VARCHAR(50) DEFAULT '',
    new_ipa TEXT DEFAULT '',
    new_tone VARCHAR(50) DEFAULT '',
    changed_by VARCHAR(200) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tasks_wordlist ON tasks(wordlist_id);
CREATE INDEX IF NOT EXISTS idx_tasks_speaker ON tasks(speaker_id);
CREATE INDEX IF NOT EXISTS idx_recordings_task ON recordings(task_id);
CREATE INDEX IF NOT EXISTS idx_segments_recording ON segments(recording_id);
CREATE INDEX IF NOT EXISTS idx_segments_status ON segments(status);
CREATE INDEX IF NOT EXISTS idx_annotations_segment ON annotations(segment_id);
CREATE INDEX IF NOT EXISTS idx_arbitrations_segment ON arbitrations(segment_id);

-- ---- 方言调查点档案 ----
-- 县（调查点行政归属）
CREATE TABLE IF NOT EXISTS counties (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    code VARCHAR(50) NOT NULL UNIQUE,
    province VARCHAR(100) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 方言分区多级树：level 1=大区，2=片，3=小片…… 调查点只能挂在叶子（最细一级）上
CREATE TABLE IF NOT EXISTS dialect_regions (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    level INT NOT NULL CHECK (level >= 1),
    parent_id BIGINT REFERENCES dialect_regions(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_dialect_regions_parent ON dialect_regions(parent_id);

-- 调查点档案；current_region_id 仅为当前归属缓存，历史结论一律以 point_assignment_history 为准
CREATE TABLE IF NOT EXISTS survey_points (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    code VARCHAR(50) NOT NULL UNIQUE,
    county_id BIGINT NOT NULL REFERENCES counties(id),
    current_region_id BIGINT REFERENCES dialect_regions(id),
    merged_into BIGINT REFERENCES survey_points(id),
    note TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_survey_points_county ON survey_points(county_id);
CREATE INDEX IF NOT EXISTS idx_survey_points_region ON survey_points(current_region_id);
CREATE INDEX IF NOT EXISTS idx_survey_points_merged ON survey_points(merged_into);

-- 别名/曾用名/异写：识别“同一个地方两种名字”的依据
CREATE TABLE IF NOT EXISTS survey_point_aliases (
    id BIGSERIAL PRIMARY KEY,
    point_id BIGINT NOT NULL REFERENCES survey_points(id) ON DELETE CASCADE,
    name VARCHAR(200) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (point_id, name)
);
CREATE INDEX IF NOT EXISTS idx_survey_point_aliases_name ON survey_point_aliases(name);

-- 归属时间线（半开区间 [valid_from, valid_to)，valid_to=NULL 表示至今）
-- 同一个点任一时点只能落在一个区；区间重叠由应用层事务检出并当场拒绝
CREATE TABLE IF NOT EXISTS point_assignment_history (
    id BIGSERIAL PRIMARY KEY,
    point_id BIGINT NOT NULL REFERENCES survey_points(id),
    region_id BIGINT NOT NULL REFERENCES dialect_regions(id),
    valid_from DATE NOT NULL,
    valid_to DATE,
    changed_by VARCHAR(200) NOT NULL DEFAULT '',
    reason TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_assignment_point ON point_assignment_history(point_id);
CREATE INDEX IF NOT EXISTS idx_assignment_region ON point_assignment_history(region_id);

-- 发音人挂到调查点（可空，兼容历史数据）
ALTER TABLE speakers ADD COLUMN IF NOT EXISTS survey_point_id BIGINT REFERENCES survey_points(id);
CREATE INDEX IF NOT EXISTS idx_speakers_survey_point ON speakers(survey_point_id);