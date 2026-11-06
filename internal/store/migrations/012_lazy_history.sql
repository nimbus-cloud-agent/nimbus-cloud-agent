CREATE INDEX outbox_session_history ON outbox_messages(session_id,created_at,id);
CREATE INDEX session_turn_starts ON session_events(session_id,seq) WHERE type='turn/start';
CREATE INDEX stream_frames_turn ON stream_frames(session_id,turn_id,id);
CREATE INDEX user_questions_turn ON user_questions(session_id,turn_id,created_at,id);

-- Native results and arguments may be JSON documents encoded in text. Parsing
-- stays inside PostgreSQL so summary reads never transfer their large bodies.
CREATE FUNCTION nimbus_try_jsonb(value text) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
BEGIN
 RETURN value::jsonb;
EXCEPTION WHEN invalid_text_representation THEN
 RETURN NULL;
END;
$$;

CREATE FUNCTION nimbus_content_text(value jsonb) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE jsonb_typeof(value)
 WHEN 'string' THEN value #>> '{}'
 WHEN 'array' THEN (SELECT string_agg(block->>'text', E'\n' ORDER BY ord)
  FROM jsonb_array_elements(value) WITH ORDINALITY AS b(block,ord)
  WHERE block->>'type'='text')
 ELSE '' END;
$$;

CREATE FUNCTION nimbus_tool_id(data jsonb) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT COALESCE(data->>'subCallId',data->>'callId',data#>>'{message,toolCallId}',data#>>'{message,source,callId}',data->>'id');
$$;

CREATE FUNCTION nimbus_tool_failed(data jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT COALESCE(data->>'isError','false')='true'
 OR COALESCE(data#>>'{message,isError}','false')='true'
 OR COALESCE(data->'error','null'::jsonb) NOT IN ('null'::jsonb,'false'::jsonb,'0'::jsonb,'""'::jsonb)
 OR COALESCE(result->>'isError','false')='true'
 OR CASE WHEN jsonb_typeof(result->'exitCode')='number' THEN (result->>'exitCode')::numeric<>0 ELSE false END
 FROM (SELECT nimbus_try_jsonb(nimbus_content_text(COALESCE(NULLIF(data#>'{message,content}','null'::jsonb),NULLIF(data->'content','null'::jsonb),data#>'{result,content}'))) AS result) parsed;
$$;

CREATE FUNCTION nimbus_argument_text(arguments jsonb, key text, max_chars integer) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE WHEN jsonb_typeof(value->key)='string' THEN left(value->>key,max_chars) END
 FROM (SELECT CASE jsonb_typeof(arguments)
  WHEN 'object' THEN arguments WHEN 'string' THEN nimbus_try_jsonb(arguments #>> '{}') END AS value) parsed;
$$;
