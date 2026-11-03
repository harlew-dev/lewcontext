-- Accounts an admin (or the operator, from the command line) suspended
-- (docs/api/admin.md#suspending-an-account): one row while suspended,
-- deleted when unsuspended or with the account. A suspended account has no
-- session (they are deleted with the suspension, and FindSession never
-- finds one), can't sign in (an honest 403, only after its password and
-- code) and gets no reset link. It is never an admin: suspending refuses
-- one, and granting refuses a suspended account.
CREATE TABLE user_suspensions (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 -- Why, as the admin wrote it: shown to admins only.
 reason text NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
 -- The admin who suspended it; null from the command line, or once that
 -- account is deleted (the admin.user.suspended event still names them).
 suspended_by bigint REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
