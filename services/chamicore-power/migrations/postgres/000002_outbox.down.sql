SET search_path TO power;

DROP INDEX IF EXISTS power.idx_outbox_unsent;
DROP TABLE IF EXISTS power.outbox;
