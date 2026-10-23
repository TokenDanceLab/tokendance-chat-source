-- +goose Up
-- Guest handle lookups and lane timelines are hot read paths. These indexes
-- keep the new "does this handle own private history" check bounded and make
-- side-lane snapshots efficient.
CREATE INDEX IF NOT EXISTS idx_messages_username
  ON messages (username, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_messages_dm_lane
  ON messages (username, to_user, thread_id, created_at DESC, seq DESC)
  WHERE deleted = false;

CREATE INDEX IF NOT EXISTS idx_messages_lane_snapshot
  ON messages (thread_id, seq DESC)
  WHERE deleted = false AND room_id = '';

CREATE INDEX IF NOT EXISTS idx_bot_runs_day_actor
  ON bot_runs (budget_day, actor_id, status);

CREATE INDEX IF NOT EXISTS idx_bot_runs_day_ip
  ON bot_runs (budget_day, ip_key)
  WHERE ip_key <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_bot_runs_day_ip;
DROP INDEX IF EXISTS idx_bot_runs_day_actor;
DROP INDEX IF EXISTS idx_messages_lane_snapshot;
DROP INDEX IF EXISTS idx_messages_dm_lane;
DROP INDEX IF EXISTS idx_messages_username;