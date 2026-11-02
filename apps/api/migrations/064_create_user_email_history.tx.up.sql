CREATE TABLE user_email_history (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 email text NOT NULL,
 released_at timestamptz NOT NULL
);
CREATE INDEX user_email_history_user_idx ON user_email_history(user_id);
CREATE INDEX user_email_history_email_released_idx ON user_email_history(email, released_at);
