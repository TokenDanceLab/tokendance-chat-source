-- Guest identity bindings for the anonymous (cookie) identity plane.
-- A browser's HttpOnly guest cookie is a 128-bit bearer secret; the first
-- handle it joins with is bound to it and cannot be re-claimed by another
-- cookie. Guests remain a weak, non-privacy identity: this only makes the same
-- browser come back as the same temporary principal.

-- +goose Up
CREATE TABLE guest_identities (
  guest_id     text PRIMARY KEY,
  handle       text NOT NULL UNIQUE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE guest_identities;
