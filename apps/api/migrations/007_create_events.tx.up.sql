CREATE TABLE events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('evt') CHECK (public_id ~ '^evt_[0-9A-Za-z]{14}$'),
 name text NOT NULL CHECK (name ~ '^[a-z][a-z_]*(\.[a-z][a-z_]*){1,2}$'),
 actor_id bigint REFERENCES users(id) ON DELETE SET NULL,
 user_id bigint REFERENCES users(id) ON DELETE CASCADE,
 workspace_id bigint REFERENCES workspaces(id) ON DELETE SET NULL,
 payload jsonb NOT NULL,
 -- The client's address for an event recorded during a request; null for the
 -- CLI and listeners. The one place an event keeps it (never in the payload).
 ip inet,
 user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 256),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_user_created_idx ON events(user_id, created_at DESC);
CREATE INDEX events_workspace_created_idx ON events(workspace_id, created_at DESC);
CREATE INDEX events_name_created_idx ON events(name, created_at DESC);
CREATE INDEX events_created_idx ON events(created_at) INCLUDE (name);
-- Each user's last sign-in, for the admin dashboard's user list.
CREATE INDEX events_logged_in_idx ON events(user_id, created_at DESC) WHERE name = 'auth.logged_in';
-- Each admin's last admin sign-in (the admin is the actor), for the Admins
-- page: events_actor_created_idx would walk every read the admin made.
CREATE INDEX events_admin_logged_in_idx ON events(actor_id, created_at DESC) WHERE name = 'admin.logged_in';
-- The admin dashboard's Events filters (docs/api/admin.md): by IP or range,
-- by the email in a payload, by the workspace in a payload (which outlives
-- the workspace), and by the actor (the account filter's other half is
-- events_user_created_idx).
CREATE INDEX events_ip_created_idx ON events(ip, created_at DESC) WHERE ip IS NOT NULL;
CREATE INDEX events_email_created_idx ON events((lower(payload->>'email')), created_at DESC) WHERE payload ? 'email';
CREATE INDEX events_payload_workspace_idx ON events((payload->>'workspace'), created_at DESC) WHERE payload ? 'workspace';
CREATE INDEX events_actor_created_idx ON events(actor_id, created_at DESC) WHERE actor_id IS NOT NULL;
-- An event's detail lists the events naming it in their request field.
CREATE INDEX events_request_idx ON events((payload->>'request')) WHERE payload ? 'request';
