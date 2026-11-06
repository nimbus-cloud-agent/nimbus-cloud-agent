-- Library: global object-storage-like file store in PostgreSQL, with
-- per-file versioning. Files are split into 1 MiB chunks (see
-- internal/store/library.go); a version's bytes are the concatenation of
-- its chunks in chunk_index order.
CREATE TABLE library_files (
  id uuid PRIMARY KEY,
  name text NOT NULL UNIQUE,
  mime_type text NOT NULL,
  size_bytes bigint NOT NULL,
  current_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE file_versions (
  id uuid PRIMARY KEY,
  file_id uuid NOT NULL REFERENCES library_files(id) ON DELETE CASCADE,
  version_number int NOT NULL,
  size_bytes bigint NOT NULL,
  sha256 text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(file_id, version_number)
);
CREATE TABLE file_chunks (
  version_id uuid NOT NULL REFERENCES file_versions(id) ON DELETE CASCADE,
  chunk_index int NOT NULL,
  data bytea NOT NULL,
  PRIMARY KEY(version_id, chunk_index)
);
-- file_versions(file_id) is not covered by that table's primary key (id),
-- so it gets its own index for the DAO's per-file version lookups. No extra
-- index on file_chunks(version_id): version_id is the leftmost column of
-- that table's primary key, which already serves version lookups.
CREATE INDEX idx_file_versions_file_id ON file_versions(file_id);
