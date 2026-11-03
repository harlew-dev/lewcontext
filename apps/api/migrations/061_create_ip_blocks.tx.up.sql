-- Client addresses and ranges an admin blocked
-- (docs/api/admin.md#ip-blocks): every server reloads the live ones every
-- 30 seconds and answers their requests 403, apart from the health checks
-- and the admin door. Each expires within 30 days, and is at most a /16
-- (IPv4) or a /32 (IPv6) wide; the server's cleanup deletes expired rows.
-- 30 days is written as 720 hours: an interval in days is calendar days in
-- the session's TimeZone, 719 hours across a spring-forward, so a 30-day
-- block would fail the CHECK there.
CREATE TABLE ip_blocks (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('ipb') CHECK (public_id ~ '^ipb_[0-9A-Za-z]{14}$'),
 network cidr NOT NULL UNIQUE CHECK (masklen(network) >= CASE family(network) WHEN 4 THEN 16 ELSE 32 END),
 reason text NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
 -- The admin who blocked it; null once that account is deleted (the
 -- admin.ip.blocked event still names them).
 created_by bigint REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 CHECK (expires_at > created_at AND expires_at <= created_at + interval '720 hours')
);
CREATE INDEX ip_blocks_expires_idx ON ip_blocks(expires_at);
