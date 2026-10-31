CREATE TABLE images (
 id uuid PRIMARY KEY,
 session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 ref jsonb NOT NULL,
 data bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX images_session ON images(session_id);
ALTER TABLE outbox_messages ADD COLUMN images jsonb NOT NULL DEFAULT '[]';
