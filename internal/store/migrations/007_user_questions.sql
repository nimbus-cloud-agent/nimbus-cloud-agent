CREATE TABLE user_questions (
 id uuid PRIMARY KEY,
 session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 turn_id uuid NOT NULL REFERENCES outbox_messages(turn_id) ON DELETE CASCADE,
 questions jsonb NOT NULL,
 answer jsonb,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','answered','dismissed','interrupted')),
 created_at timestamptz NOT NULL DEFAULT now(),
 resolved_at timestamptz
);
CREATE INDEX user_questions_session ON user_questions(session_id,created_at);
