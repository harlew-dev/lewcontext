CREATE TABLE users (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('usr') CHECK (public_id ~ '^usr_[0-9A-Za-z]{14}$'),
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
 email text NOT NULL UNIQUE,
 password_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
