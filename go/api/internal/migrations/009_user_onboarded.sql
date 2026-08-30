-- Tracks whether the user has completed the first-run web onboarding.
-- Existing accounts are backfilled to true so only users created after this
-- migration (new signups, who start at the column's FALSE default) see the
-- onboarding overlay. The web UI flips it to true via POST /v1/me/onboarding
-- once the welcome + spotlight tour finishes or is dismissed.

ALTER TABLE users ADD COLUMN IF NOT EXISTS onboarded BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE users SET onboarded = TRUE WHERE onboarded = FALSE;
