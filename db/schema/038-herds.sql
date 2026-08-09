-- Herds: durable packs of terminal members (control room over dtach sessions).
-- Member identity survives close/respawn; terminal_id is nullable and unique when set.

CREATE TABLE herds (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    default_cwd TEXT NOT NULL DEFAULT '',
    default_command TEXT NOT NULL DEFAULT '',
    default_env TEXT NOT NULL DEFAULT '{}',
    lifecycle TEXT NOT NULL DEFAULT 'active' CHECK (lifecycle IN ('active', 'archived')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Active herd names are unique; archived herds may reuse a name after archive.
CREATE UNIQUE INDEX idx_herds_active_name ON herds(name) WHERE lifecycle = 'active';

CREATE TABLE herd_members (
    id TEXT PRIMARY KEY,
    herd_id TEXT NOT NULL REFERENCES herds(id) ON DELETE CASCADE,
    terminal_id TEXT,
    conversation_id TEXT,
    label TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    recipe TEXT NOT NULL DEFAULT '{}',
    desired_state TEXT NOT NULL DEFAULT 'closed' CHECK (desired_state IN ('open', 'closed')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- One live terminal belongs to at most one herd member (SQLite allows multiple NULLs).
CREATE UNIQUE INDEX idx_herd_members_terminal ON herd_members(terminal_id) WHERE terminal_id IS NOT NULL;

CREATE INDEX idx_herd_members_herd ON herd_members(herd_id, sort_order);
