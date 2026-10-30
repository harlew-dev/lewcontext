CREATE TABLE projects (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('prj') CHECK (public_id ~ '^prj_[0-9A-Za-z]{14}$'),
 workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
 description text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
 status text NOT NULL DEFAULT 'In progress' CHECK (status IN ('In progress','Planned','Completed')),
 color text NOT NULL DEFAULT 'violet' CHECK (color IN ('violet','blue','amber','emerald','rose')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX projects_workspace_updated_idx ON projects(workspace_id, updated_at DESC);
