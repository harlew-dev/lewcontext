-- An admin's opt-in to alert email: a row is a signal they want, the lowest
-- level they want it at, and the kinds of event (opened, escalated, …) they
-- want about it; an empty list sends nothing. No row means no email about that
-- signal, and nobody is subscribed by default. Gone with the admin.
CREATE TABLE admin_alert_subscriptions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    admin_user_id bigint NOT NULL REFERENCES admins(user_id) ON DELETE CASCADE,
    signal text NOT NULL CHECK (length(signal) BETWEEN 1 AND 100),
    min_level text NOT NULL CHECK (min_level IN ('warn', 'critical')),
    events text[] NOT NULL DEFAULT '{}' CHECK (cardinality(events) <= 7),
    UNIQUE (admin_user_id, signal)
);
