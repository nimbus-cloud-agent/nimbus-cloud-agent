-- Runtime-created images are pushed by the agent runtime and replayed on every
-- later turn, so the content id must be unique per session.
CREATE UNIQUE INDEX images_attachment ON images (session_id, (ref->>'attachmentId'));
