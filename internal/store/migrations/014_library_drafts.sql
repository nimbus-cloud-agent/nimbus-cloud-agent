-- A conflicting edit saved instead of published: the user keeps their text and
-- the agent later merges it against the current version. Text files only, one
-- open draft per file.
CREATE TABLE library_drafts (
  id uuid PRIMARY KEY,
  file_id uuid NOT NULL UNIQUE REFERENCES library_files(id) ON DELETE CASCADE,
  base_version int NOT NULL,
  mime_type text NOT NULL,
  size_bytes bigint NOT NULL,
  content text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
