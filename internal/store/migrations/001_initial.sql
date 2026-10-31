CREATE TABLE IF NOT EXISTS sessions (
 id uuid PRIMARY KEY, title text NOT NULL DEFAULT '新对话', agent_state text NOT NULL DEFAULT 'idle' CHECK(agent_state IN ('idle','starting','running','flushing','stopping','error')),
 synced_seq bigint NOT NULL DEFAULT -1, options jsonb NOT NULL DEFAULT '{"model":"deepseek-chat","thinking":"off","plan":false,"mode":"standard"}',
 header jsonb, created_at timestamptz NOT NULL DEFAULT now(), last_active_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS session_events (
 session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, seq bigint NOT NULL, type text NOT NULL, payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(session_id,seq)
);
CREATE TABLE IF NOT EXISTS outbox_messages (
 id uuid PRIMARY KEY, session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, turn_id uuid NOT NULL UNIQUE, client_msg_id text NOT NULL,
 content text NOT NULL, options jsonb NOT NULL, status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','starting','delivered','failed')),
 delivered_seq bigint, error text NOT NULL DEFAULT '', outcome text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(session_id,client_msg_id)
);
CREATE TABLE IF NOT EXISTS sandboxes (
 id uuid PRIMARY KEY, session_id uuid NOT NULL UNIQUE REFERENCES sessions(id) ON DELETE CASCADE, e2b_sandbox_id text NOT NULL,
 state text NOT NULL CHECK(state IN ('provisioning','running','paused','killed','error')), info jsonb NOT NULL DEFAULT '{}', error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), last_active_at timestamptz NOT NULL DEFAULT now(), started_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS settings (key text PRIMARY KEY, value jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS skills (id uuid PRIMARY KEY, name text NOT NULL UNIQUE, description text NOT NULL DEFAULT '', enabled boolean NOT NULL DEFAULT true);
CREATE TABLE IF NOT EXISTS skill_files (skill_id uuid NOT NULL REFERENCES skills(id) ON DELETE CASCADE, path text NOT NULL, content text NOT NULL CHECK(octet_length(content)<=5242880), PRIMARY KEY(skill_id,path));
CREATE TABLE IF NOT EXISTS mcp_servers (id uuid PRIMARY KEY, name text NOT NULL UNIQUE, url text NOT NULL, enabled boolean NOT NULL DEFAULT true, oauth_state text NOT NULL DEFAULT 'none', oauth_data jsonb NOT NULL DEFAULT '{}');
CREATE OR REPLACE FUNCTION nimbus_immutable_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'session events are append-only'; END $$;
DROP TRIGGER IF EXISTS immutable_event ON session_events;
CREATE TRIGGER immutable_event BEFORE UPDATE ON session_events FOR EACH ROW EXECUTE FUNCTION nimbus_immutable_event();
CREATE INDEX IF NOT EXISTS outbox_queue ON outbox_messages(status,created_at);
