CREATE TABLE stream_frames (
 id bigserial PRIMARY KEY, session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 turn_id uuid NOT NULL REFERENCES outbox_messages(turn_id) ON DELETE CASCADE,
 payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX stream_frames_session ON stream_frames(session_id,id);
CREATE TRIGGER immutable_frame BEFORE UPDATE ON stream_frames FOR EACH ROW EXECUTE FUNCTION nimbus_immutable_event();
