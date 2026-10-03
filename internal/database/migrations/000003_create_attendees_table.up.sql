CREATE TABLE IF NOT EXISTS attendees (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    event_id   BIGINT      NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT attendees_event_user_unique UNIQUE (event_id, user_id)
);

CREATE INDEX IF NOT EXISTS attendees_user_id_idx ON attendees (user_id);
