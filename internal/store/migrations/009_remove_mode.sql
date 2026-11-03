DROP TRIGGER IF EXISTS session_preset_guard ON sessions;
DROP TRIGGER IF EXISTS outbox_preset_guard ON outbox_messages;
DROP FUNCTION IF EXISTS nimbus_session_preset_guard();
DROP FUNCTION IF EXISTS nimbus_outbox_preset_guard();
ALTER TABLE sessions DROP COLUMN IF EXISTS locked_mode;
ALTER TABLE sessions ALTER COLUMN options SET DEFAULT '{"model":"deepseek-flash","thinking":"off","plan":false}';
UPDATE sessions SET options = options - 'mode';
UPDATE outbox_messages SET options = options - 'mode';
