-- Alert receivers an operator manages in the dashboard (docs/api/monitoring.md):
-- webhooks, which get the signals and event kinds they were set to, and
-- heartbeats, which the alert leader GETs after each good evaluation. The URL
-- and a webhook's signing key are sealed with APP_KEY, bound to this row's
-- public ID, and never returned by a list (a URL can itself be a secret).
-- Each webhook has its own key, with a public key_id to tell which to verify
-- with. An empty signals or events list sends nothing, never "everything".
CREATE TABLE alert_webhooks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE DEFAULT public_id('whk') CHECK (public_id ~ '^whk_[0-9A-Za-z]{14}$'),
    kind text NOT NULL CHECK (kind IN ('webhook', 'heartbeat')),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    sealed_url bytea NOT NULL,
    format text NOT NULL DEFAULT '' CHECK (format IN ('', 'json', 'slack')),
    sealed_key bytea,
    key_id text NOT NULL DEFAULT '' CHECK (key_id = '' OR key_id ~ '^key_[0-9A-Za-z]{8}$'),
    sealed_overlap_key bytea,
    overlap_key_id text NOT NULL DEFAULT '' CHECK (overlap_key_id = '' OR overlap_key_id ~ '^key_[0-9A-Za-z]{8}$'),
    overlap_until timestamptz,
    signals text[] NOT NULL DEFAULT '{}' CHECK (cardinality(signals) <= 200),
    events text[] NOT NULL DEFAULT '{}' CHECK (cardinality(events) <= 7),
    enabled boolean NOT NULL DEFAULT true,
    -- Bumped when the URL, format or key is replaced: queued deliveries made
    -- before then are skipped, never sent to the new receiver.
    delivery_revision bigint NOT NULL DEFAULT 1 CHECK (delivery_revision > 0),
    -- Optimistic concurrency for edits.
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    -- Posts per signal in the last hour (bounded, trimmed on write): the
    -- cap is 12 an hour per signal per webhook.
    post_times jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(post_times) = 'object'),
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (kind = 'webhook' AND format IN ('json', 'slack') AND sealed_key IS NOT NULL AND key_id <> ''
        OR kind = 'heartbeat' AND format = '' AND sealed_key IS NULL AND key_id = '' AND cardinality(signals) = 0 AND cardinality(events) = 0),
    CHECK ((sealed_overlap_key IS NULL) = (overlap_until IS NULL))
);
CREATE UNIQUE INDEX alert_webhooks_name_idx ON alert_webhooks(lower(name));
CREATE INDEX alert_webhooks_kind_idx ON alert_webhooks(kind) WHERE enabled;
