ALTER TABLE sessions ADD COLUMN locked_mode text CHECK (locked_mode IN ('standard', 'ptc'));

-- Old versions allowed mixed presets. Continue the last delivered preset;
-- otherwise use the first submitted message. Never rewrite native history.
UPDATE sessions s SET locked_mode = COALESCE(
  (SELECT o.options->>'mode' FROM outbox_messages o
   WHERE o.session_id=s.id AND o.delivered_seq IS NOT NULL
   ORDER BY o.delivered_seq DESC LIMIT 1),
  (SELECT o.options->>'mode' FROM outbox_messages o
   WHERE o.session_id=s.id ORDER BY o.created_at, o.id LIMIT 1),
  s.options->>'mode'
) WHERE s.header IS NOT NULL OR EXISTS (SELECT 1 FROM outbox_messages o WHERE o.session_id=s.id);
UPDATE sessions SET options=jsonb_set(options, '{mode}', to_jsonb(locked_mode)) WHERE locked_mode IS NOT NULL;
UPDATE outbox_messages o SET status='failed', outcome='error',
  error='会话工具模式已固定；此消息使用了不同模式，请新建会话后发送'
FROM sessions s WHERE o.session_id=s.id AND o.status IN ('queued', 'starting')
  AND o.options->>'mode' IS DISTINCT FROM s.locked_mode;

CREATE FUNCTION nimbus_session_preset_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.locked_mode IS NOT NULL AND NEW.locked_mode IS DISTINCT FROM OLD.locked_mode THEN
    RAISE EXCEPTION 'session preset is locked';
  END IF;
  IF NEW.locked_mode IS NOT NULL AND NEW.options->>'mode' IS DISTINCT FROM NEW.locked_mode THEN
    RAISE EXCEPTION 'session preset is locked';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER session_preset_guard BEFORE UPDATE ON sessions
FOR EACH ROW EXECUTE FUNCTION nimbus_session_preset_guard();

-- The first accepted message freezes the preset, including queued/failed turns.
-- Serializing on the session row closes the PATCH vs. first-send race.
CREATE FUNCTION nimbus_outbox_preset_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE preset text;
BEGIN
  IF TG_OP='UPDATE' THEN
    IF NEW.session_id=OLD.session_id AND NEW.options=OLD.options
       AND NEW.status NOT IN ('queued', 'starting') THEN RETURN NEW; END IF;
  END IF;
  SELECT locked_mode INTO preset FROM sessions WHERE id=NEW.session_id FOR UPDATE;
  IF NEW.options->>'mode' IS NULL OR NEW.options->>'mode' NOT IN ('standard', 'ptc') THEN
    RAISE EXCEPTION 'invalid session preset';
  END IF;
  IF preset IS NULL THEN
    UPDATE sessions SET locked_mode=NEW.options->>'mode',
      options=jsonb_set(options, '{mode}', NEW.options->'mode') WHERE id=NEW.session_id;
  ELSIF preset IS DISTINCT FROM NEW.options->>'mode' THEN
    RAISE EXCEPTION 'session preset is locked';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER outbox_preset_guard BEFORE INSERT OR UPDATE OF options, session_id, status ON outbox_messages
FOR EACH ROW EXECUTE FUNCTION nimbus_outbox_preset_guard();
