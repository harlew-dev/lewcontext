CREATE TABLE users (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('usr') CHECK (public_id ~ '^usr_[0-9A-Za-z]{14}$'),
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
 email text NOT NULL UNIQUE,
 password_hash text NOT NULL,
 -- When the account proved it owns the email with a code (or a password
 -- reset link). Null for accounts made while EMAIL_VERIFICATION was off.
 email_verified_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
