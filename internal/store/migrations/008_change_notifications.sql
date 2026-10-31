-- Notification payloads contain identities only. The schema-specific channel
-- keeps independent installations and disposable test schemas isolated.
CREATE FUNCTION nimbus_notify_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sid uuid;
BEGIN
  IF TG_ARGV[0] = 'session' THEN
    IF TG_OP = 'DELETE' THEN sid := OLD.id; ELSE sid := NEW.id; END IF;
  ELSE
    IF TG_OP = 'DELETE' THEN sid := OLD.session_id; ELSE sid := NEW.session_id; END IF;
  END IF;
  IF TG_OP = 'UPDATE' AND NEW IS NOT DISTINCT FROM OLD THEN RETURN NULL; END IF;
  PERFORM pg_notify('nimbus_changes_' || md5(TG_TABLE_SCHEMA),
    json_build_object('session_id', sid, 'kind', TG_ARGV[0])::text);
  RETURN NULL;
END $$;

CREATE TRIGGER notify_session AFTER INSERT OR UPDATE OR DELETE ON sessions
FOR EACH ROW EXECUTE FUNCTION nimbus_notify_change('session');
CREATE TRIGGER notify_sandbox AFTER INSERT OR UPDATE OR DELETE ON sandboxes
FOR EACH ROW EXECUTE FUNCTION nimbus_notify_change('sandbox');
CREATE TRIGGER notify_outbox AFTER INSERT OR UPDATE OR DELETE ON outbox_messages
FOR EACH ROW EXECUTE FUNCTION nimbus_notify_change('outbox');
CREATE TRIGGER notify_question AFTER INSERT OR UPDATE OR DELETE ON user_questions
FOR EACH ROW EXECUTE FUNCTION nimbus_notify_change('question');
CREATE TRIGGER notify_event AFTER INSERT OR UPDATE OR DELETE ON session_events
FOR EACH ROW EXECUTE FUNCTION nimbus_notify_change('event');
CREATE TRIGGER notify_frame AFTER INSERT OR UPDATE OR DELETE ON stream_frames
FOR EACH ROW EXECUTE FUNCTION nimbus_notify_change('frame');
