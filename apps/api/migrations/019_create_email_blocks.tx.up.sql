-- The block list: addresses whose owner asked, from a block link, for no
-- more email and no sign-up. Normalised as sign-up stores emails (trimmed,
-- lower-case), and kept for as long as the address is blocked: a
-- suppression list, never pruned. A blocked address never has an account.
CREATE TABLE email_blocks (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 email text NOT NULL UNIQUE CHECK (email = lower(btrim(email))),
 created_at timestamptz NOT NULL DEFAULT now()
);
