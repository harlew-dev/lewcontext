CREATE TABLE email_changes (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 session_hash text NOT NULL REFERENCES sessions(token_hash) ON DELETE CASCADE,
 previous_email text NOT NULL,
 email text NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
 email_version bigint NOT NULL CHECK (email_version >= 0),
 request_id text NOT NULL UNIQUE CHECK (request_id ~ '^evt_[0-9A-Za-z]{14}$'),
 code_hash text,
 attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 4),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 CHECK (expires_at > created_at AND expires_at <= created_at + interval '15 minutes')
);
CREATE INDEX email_changes_expires_idx ON email_changes(expires_at);
CREATE INDEX email_changes_email_idx ON email_changes(email);
