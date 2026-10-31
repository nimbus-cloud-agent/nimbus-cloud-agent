CREATE OR REPLACE FUNCTION nimbus_no_direct_event_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF pg_trigger_depth() < 2 THEN RAISE EXCEPTION 'session events can only be deleted by deleting their session'; END IF; RETURN OLD; END $$;
CREATE TRIGGER immutable_event_delete BEFORE DELETE ON session_events FOR EACH ROW EXECUTE FUNCTION nimbus_no_direct_event_delete();
CREATE TRIGGER immutable_frame_delete BEFORE DELETE ON stream_frames FOR EACH ROW EXECUTE FUNCTION nimbus_no_direct_event_delete();
CREATE OR REPLACE FUNCTION nimbus_skill_file_limit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM id FROM skills WHERE id=NEW.skill_id FOR UPDATE;
IF (SELECT count(*) FROM skill_files WHERE skill_id=NEW.skill_id)>100 THEN RAISE EXCEPTION 'at most 100 files per skill'; END IF; RETURN NEW; END $$;
CREATE TRIGGER skill_file_limit AFTER INSERT ON skill_files FOR EACH ROW EXECUTE FUNCTION nimbus_skill_file_limit();
