-- Auth tables for OAuth authentication
-- Supports multiple OAuth providers (GitHub initially)

-- Users table: stores authenticated user profiles
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,                           -- Internal UUID
    provider TEXT NOT NULL,                        -- OAuth provider: 'github'
    provider_id TEXT NOT NULL,                     -- User ID from OAuth provider
    email TEXT NOT NULL,                           -- User email (from OAuth)
    name TEXT,                                     -- Display name
    avatar_url TEXT,                               -- Profile picture URL
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_login_at DATETIME,
    UNIQUE(provider, provider_id)                  -- One user per provider account
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_provider ON users(provider, provider_id);

-- Sessions table: tracks active login sessions
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,                           -- Session UUID
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL,                       -- Mobile device identifier
    device_name TEXT,                              -- Human-readable device name
    refresh_token_hash TEXT NOT NULL,              -- Hashed refresh token for validation
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,                  -- Refresh token expiry
    last_active_at DATETIME,                       -- Last API activity
    revoked_at DATETIME                            -- NULL = active, set = revoked
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_device_id ON sessions(device_id);
