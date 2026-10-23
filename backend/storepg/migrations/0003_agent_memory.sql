-- Durable, explicitly-saved assistant memories (Agent 2.0 P0).
-- Facts are owner-scoped and small; they are injected into private runs as
-- data, never as instructions, and never into public rooms.

-- +goose Up
CREATE TABLE agent_memories (
  id                text PRIMARY KEY,
  owner_actor_id    text NOT NULL CHECK (owner_actor_id <> ''),
  scope             text NOT NULL CHECK (scope IN ('lane', 'actor', 'room')),
  thread_id         text NOT NULL DEFAULT '',
  kind              text NOT NULL CHECK (kind IN ('profile', 'preference', 'instruction', 'project', 'event', 'summary')),
  content           text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 500),
  content_hash      text NOT NULL CHECK (char_length(content_hash) = 64),
  confidence        real NOT NULL DEFAULT 0.7 CHECK (confidence >= 0 AND confidence <= 1),
  importance        smallint NOT NULL DEFAULT 3 CHECK (importance BETWEEN 1 AND 5),
  sensitivity       smallint NOT NULL DEFAULT 0 CHECK (sensitivity IN (0, 1)),
  source            text NOT NULL CHECK (source IN ('explicit_user', 'bot_tool', 'auto_extract', 'import', 'summary')),
  source_run_id     text NOT NULL DEFAULT '',
  source_message_id text NOT NULL DEFAULT '',
  status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'retracted', 'expired')),
  superseded_by     text,
  pinned            boolean NOT NULL DEFAULT false,
  valid_from        timestamptz NOT NULL DEFAULT now(),
  valid_to          timestamptz,
  expires_at        timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  CHECK (scope <> 'actor' OR thread_id = ''),
  CHECK (scope <> 'room' OR thread_id = '')
);

CREATE UNIQUE INDEX agent_memories_dedup
  ON agent_memories (owner_actor_id, scope, thread_id, content_hash)
  WHERE status = 'active';

CREATE INDEX agent_memories_owner_active
  ON agent_memories (owner_actor_id, scope, updated_at DESC)
  WHERE status = 'active';

CREATE INDEX agent_memories_expiry
  ON agent_memories (expires_at)
  WHERE status = 'active' AND expires_at IS NOT NULL;

CREATE TABLE agent_memory_events (
  id         text PRIMARY KEY,
  memory_id  text NOT NULL,
  actor_id   text NOT NULL,
  action     text NOT NULL,
  detail     text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX agent_memory_events_memory ON agent_memory_events (memory_id, created_at DESC);

-- +goose Down
DROP TABLE agent_memory_events;
DROP TABLE agent_memories;
