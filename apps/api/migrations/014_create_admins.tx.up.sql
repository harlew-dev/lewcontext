-- Accounts granted admin from the command line (cmd/admin grant), never
-- through the API. mfa_setup_until is until when the admin may set up MFA at
-- the admin door (their first admin sign-in): set by grant and by an MFA
-- reset, cleared once MFA is on. Null: no setup at the door. role is what
-- the admin may do: a viewer reads, an operator may also act (the actions
-- each name the role they need); set from the command line only.
CREATE TABLE admins (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 mfa_setup_until timestamptz,
 role text NOT NULL DEFAULT 'viewer' CHECK (role IN ('viewer', 'operator')),
 created_at timestamptz NOT NULL DEFAULT now()
);
