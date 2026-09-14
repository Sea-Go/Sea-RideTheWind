-- Apply before enabling a Favorite RPC that saves Article published revisions.
-- Existing favorites remain NULL: an old title, status, or current Article
-- pointer cannot reconstruct the revision observed at creation time.
ALTER TABLE favorite_item
    ADD COLUMN IF NOT EXISTS target_revision varchar(96);
