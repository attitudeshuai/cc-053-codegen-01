-- Dialect Survey Point Archive - Schema
-- 方言调查点档案：县、多级方言区、调查点、归属时间线、条目快照、合并审计
-- This file is for reference; actual migrations run via internal/database/database.go

-- 县（行政区划，点挂在哪个县）
CREATE TABLE IF NOT EXISTS counties (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(100) NOT NULL,
    code       VARCHAR(50)  NOT NULL UNIQUE,
    note       VARCHAR(500) DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 多级方言区（邻接表自引用）：大区 > 片 > 小片 …
-- 一个调查点只能挂在“叶子级”区上；level 越小层级越高（1=一级区）
CREATE TABLE IF NOT EXISTS dialect_regions (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(100) NOT NULL,
    code       VARCHAR(50)   NOT NULL UNIQUE,
    level      INT NOT NULL CHECK (level >= 1),
    parent_id  BIGINT REFERENCES dialect_regions(id),
    path       BIGINT[] NOT NULL DEFAULT '{}',   -- 从一级区到本区的 id 链
    note       VARCHAR(500) DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT regions_leaf_check CHECK (parent_id IS NOT NULL OR level = 1)
);

CREATE INDEX IF NOT EXISTS idx_regions_parent ON dialect_regions(parent_id);

-- 调查点（每点归一个县；属哪个叶子方言区随时间演进，存时间线，不直接在本列）
CREATE TABLE IF NOT EXISTS survey_points (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(100) NOT NULL,           -- 规范名称
    code       VARCHAR(50)  NOT NULL UNIQUE,    -- 唯一编码，合并后作废码也不再复用
    county_id  BIGINT NOT NULL REFERENCES counties(id),
    status     VARCHAR(20) NOT NULL DEFAULT 'active'
               CHECK (status IN ('active','merged')),
    merged_into_id BIGINT REFERENCES survey_points(id),
    note       VARCHAR(500) DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_points_county ON survey_points(county_id);
CREATE INDEX IF NOT EXISTS idx_points_merged_into ON survey_points(merged_into_id);

-- 名字指纹：同一地方写成两种名字（简繁/异体/空白/标点/大小写差异）也能认出是同一个
CREATE TABLE IF NOT EXISTS survey_point_aliases (
    id           BIGSERIAL PRIMARY KEY,
    point_id     BIGINT NOT NULL REFERENCES survey_points(id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL,          -- 原始写法
    fingerprint  VARCHAR(100) NOT NULL,          -- 规范化指纹
    source       VARCHAR(20) NOT NULL DEFAULT 'manual'
                 CHECK (source IN ('manual','canonical','merge'))
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_aliases_fingerprint
    ON survey_point_aliases(fingerprint);
CREATE UNIQUE INDEX IF NOT EXISTS ux_aliases_point_name
    ON survey_point_aliases(point_id, fingerprint);
CREATE INDEX IF NOT EXISTS idx_aliases_point ON survey_point_aliases(point_id);

-- 归属时间线（SCD2）：同一调查点在任意时刻只能挂在一个下级（叶子）区上。
-- 调整归属必须写明 effective_date；区间 [effective_date, end_date) 左闭右开，
-- end_date 为 NULL 表示现行。历史区间只许在空档处补录，覆盖已有区间一律拒绝。
CREATE TABLE IF NOT EXISTS point_assignments (
    id              BIGSERIAL PRIMARY KEY,
    point_id        BIGINT NOT NULL REFERENCES survey_points(id),
    region_id       BIGINT NOT NULL REFERENCES dialect_regions(id),
    effective_date  DATE NOT NULL,
    end_date        DATE,                          -- NULL = 现行
    created_by      VARCHAR(200) NOT NULL DEFAULT '',
    note            VARCHAR(500) DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 每点至多一条现行区间
CREATE UNIQUE INDEX IF NOT EXISTS ux_assignments_current
    ON point_assignments(point_id) WHERE (end_date IS NULL);
-- 同一点同一生效日不许有两条（防止当场挂重）
CREATE UNIQUE INDEX IF NOT EXISTS ux_assignments_eff
    ON point_assignments(point_id, effective_date);
CREATE INDEX IF NOT EXISTS idx_assignments_region ON point_assignments(region_id);

-- 已发出去的每条说法/记录：建立时按当时的归属做快照，之后归属怎么调都不变
CREATE TABLE IF NOT EXISTS point_records (
    id               BIGSERIAL PRIMARY KEY,
    point_id         BIGINT NOT NULL REFERENCES survey_points(id),
    content          TEXT NOT NULL,
    record_date      DATE NOT NULL,             -- 说法成立/发出的日期
    snapshot_county  VARCHAR(100) NOT NULL,     -- 当时归属的县名（快照）
    snapshot_region  VARCHAR(100) NOT NULL,     -- 当时归属的叶子区名（快照）
    snapshot_path    TEXT NOT NULL DEFAULT '',  -- 当时区名全路径（一级/片/小片）
    created_by       VARCHAR(200) NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_records_point ON point_records(point_id);
CREATE INDEX IF NOT EXISTS idx_records_date ON point_records(record_date);

-- 合并审计：两个名字其实是同一个地方 -> 合成一个，留痕
CREATE TABLE IF NOT EXISTS point_merges (
    id                 BIGSERIAL PRIMARY KEY,
    surviving_point_id BIGINT NOT NULL REFERENCES survey_points(id),
    merged_point_id    BIGINT NOT NULL REFERENCES survey_points(id),
    surviving_before   INT NOT NULL,
    merged_before      INT NOT NULL,
    total_after        INT NOT NULL,
    merged_by          VARCHAR(200) NOT NULL DEFAULT '',
    note               VARCHAR(500) DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 快照不可变：point_records 不许改归属快照、不许删除；
-- 合并档案时只转移 point_id（触发器对该列放行）。
CREATE OR REPLACE FUNCTION forbid_snapshot_mutation() RETURNS trigger AS $$
BEGIN
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
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_records_no_update ON point_records;
CREATE TRIGGER trg_records_no_update BEFORE UPDATE ON point_records
    FOR EACH ROW EXECUTE FUNCTION forbid_snapshot_mutation();

DROP TRIGGER IF EXISTS trg_records_no_delete ON point_records;
CREATE TRIGGER trg_records_no_delete BEFORE DELETE ON point_records
    FOR EACH ROW EXECUTE FUNCTION forbid_snapshot_mutation();
