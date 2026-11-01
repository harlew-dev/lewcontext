-- Sign-ups waiting for the code sent to their email (EMAIL_VERIFICATION=on):
-- no users row exists until the code is right. Each row belongs to the
-- browser that started it, by the SHA-256 digest of its cookie's token, so
-- several can exist for one email. request_id is the evt_ ID of the latest
-- auth.registration.requested, by which the listener finds the row.
-- code_hash is an HMAC of the code under a key from APP_KEY; for an email
-- that already has an account it is the HMAC of a code nobody is sent.
CREATE TABLE pending_registrations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 email text NOT NULL,
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
 password_hash text NOT NULL,
 token_hash text NOT NULL UNIQUE,
 request_id text NOT NULL UNIQUE,
 code_hash text,
 attempts smallint NOT NULL DEFAULT 0,
 -- The policy versions agreed to (POLICIES=on), copied to users by verify.
 terms_of_service_version date,
 privacy_policy_version date,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 CHECK ((terms_of_service_version IS NULL) = (privacy_policy_version IS NULL))
);
CREATE INDEX pending_registrations_email_idx ON pending_registrations(email);
CREATE INDEX pending_registrations_expires_at_idx ON pending_registrations(expires_at);
