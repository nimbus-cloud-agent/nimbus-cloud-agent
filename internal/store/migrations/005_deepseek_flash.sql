ALTER TABLE sessions ALTER COLUMN options SET DEFAULT '{"model":"deepseek-flash","thinking":"off","plan":false,"mode":"standard"}';
UPDATE sessions SET options=jsonb_set(options, '{model}', '"deepseek-flash"');
-- Only undelivered, compatible messages can be retried. Preserve delivered
-- snapshots and native events exactly as recorded by the original model.
UPDATE outbox_messages o SET options=jsonb_set(o.options, '{model}', '"deepseek-flash"')
FROM sessions s WHERE o.session_id=s.id AND o.delivered_seq IS NULL
  AND o.options->>'mode'=s.locked_mode;
