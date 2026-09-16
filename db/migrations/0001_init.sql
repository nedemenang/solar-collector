CREATE TABLE IF NOT EXISTS readings (
    id        TEXT PRIMARY KEY,
    source    TEXT NOT NULL,
    tag       TEXT NOT NULL,
    value     DOUBLE PRECISION NOT NULL,
    ts        TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_readings_tag_ts ON readings (tag, ts DESC);

CREATE TABLE IF NOT EXISTS alarms (
    id         TEXT PRIMARY KEY,
    tag        TEXT NOT NULL,
    condition  TEXT NOT NULL,
    value      DOUBLE PRECISION NOT NULL,
    raised_at  TIMESTAMPTZ NOT NULL,
    cleared_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS rollups (
    tag          TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end   TIMESTAMPTZ NOT NULL,
    min_value    DOUBLE PRECISION NOT NULL,
    max_value    DOUBLE PRECISION NOT NULL,
    avg_value    DOUBLE PRECISION NOT NULL,
    sample_count INTEGER NOT NULL,
    PRIMARY KEY (tag, window_start)
);
