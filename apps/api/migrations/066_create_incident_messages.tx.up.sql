CREATE TABLE incident_messages (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE DEFAULT public_id('msg') CHECK (public_id ~ '^msg_[0-9A-Za-z]{14}$'),
    cycle_id bigint NOT NULL REFERENCES incident_cycles(id) ON DELETE CASCADE,
    author_id bigint REFERENCES users(id) ON DELETE SET NULL,
    body text CHECK (length(body) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    redacted_at timestamptz,
    redacted_by bigint REFERENCES users(id) ON DELETE SET NULL,
    CHECK ((body IS NULL) = (redacted_at IS NOT NULL)),
    CHECK (redacted_at IS NOT NULL OR redacted_by IS NULL),
    UNIQUE (cycle_id, id)
);
CREATE INDEX incident_messages_cycle_created_idx ON incident_messages(cycle_id, created_at DESC, id DESC);
ALTER TABLE incident_cycles ADD CONSTRAINT incident_cycles_resolution_message_fkey
    FOREIGN KEY (id, resolution_message_id) REFERENCES incident_messages(cycle_id, id);
