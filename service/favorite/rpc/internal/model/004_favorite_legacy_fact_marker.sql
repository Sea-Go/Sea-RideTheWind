-- An explicit, evidence-backed exception for business rows created before
-- Favorite had an Outbox. This migration creates NO markers automatically:
-- absence of an assertion cannot distinguish legitimate old data from a
-- modern write failure. The migration owner inserts reviewed snapshots only.
CREATE TABLE IF NOT EXISTS favorite_legacy_fact_marker (
    favorite_id bigint PRIMARY KEY CHECK (favorite_id > 0),
    user_id bigint NOT NULL CHECK (user_id > 0),
    folder_id bigint NOT NULL CHECK (folder_id > 0),
    target_type text NOT NULL CHECK (target_type <> ''),
    target_id text NOT NULL CHECK (target_id <> ''),
    target_revision varchar(96),
    source_snapshot_sha256 char(64) NOT NULL CHECK (source_snapshot_sha256 ~ '^[a-f0-9]{64}$'),
    approval_ref text NOT NULL CHECK (approval_ref <> ''),
    approved_at timestamptz NOT NULL DEFAULT now(),
    CHECK (target_revision IS NULL OR (target_type = 'article' AND target_revision <> ''))
);

CREATE OR REPLACE FUNCTION reject_favorite_legacy_marker_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'favorite legacy fact marker is immutable';
END;
$$;
DROP TRIGGER IF EXISTS favorite_legacy_marker_immutable ON favorite_legacy_fact_marker;
CREATE TRIGGER favorite_legacy_marker_immutable
BEFORE UPDATE OR DELETE ON favorite_legacy_fact_marker
FOR EACH ROW EXECUTE FUNCTION reject_favorite_legacy_marker_change();
