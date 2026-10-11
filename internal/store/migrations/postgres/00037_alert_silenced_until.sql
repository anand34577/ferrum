-- +goose Up
-- When a silence ends on its own. NULL (with status = 'silenced') is the
-- original open-ended mute: it holds until someone un-silences the alert or
-- its condition clears. A timestamp is a timed mute ("silence for 4 hours"):
-- the alert evaluator flips the alert back to active once it passes.
ALTER TABLE alert_instances ADD COLUMN silenced_until TEXT;

-- +goose Down
ALTER TABLE alert_instances DROP COLUMN silenced_until;
