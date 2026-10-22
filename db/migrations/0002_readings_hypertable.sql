CREATE EXTENSION IF NOT EXISTS timescaledb;

-- create_hypertable requires every unique/primary key index on the table
-- to include the partitioning column, so the id-only primary key from
-- 0001 has to widen to (id, ts) first. id alone is still enough to keep
-- replay idempotent: a given reading ID is only ever written once, with
-- one fixed ts (see internal/sink/postgres.go).
ALTER TABLE readings DROP CONSTRAINT readings_pkey;
ALTER TABLE readings ADD PRIMARY KEY (id, ts);

SELECT create_hypertable('readings', 'ts', migrate_data => TRUE, if_not_exists => TRUE);
