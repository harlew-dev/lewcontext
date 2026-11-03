-- Block links: one per blockable email sent (an invitation), so whoever
-- received it can block the address it went to, or unblock it, for a year.
-- Only the SHA-256 digest of the link's token is stored. The address is kept
-- because the link's page shows it and checks it against users; the server's
-- cleanup deletes rows past expires_at.
CREATE TABLE email_block_links (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 token_hash text NOT NULL UNIQUE,
 email text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX email_block_links_expires_at_idx ON email_block_links(expires_at);
-- The admin dashboard's Email blocks page shows each blocked address's
-- newest live link's expiry: by address, then expiry.
CREATE INDEX email_block_links_email_idx ON email_block_links(email, expires_at);
