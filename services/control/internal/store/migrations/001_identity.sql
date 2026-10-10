CREATE TABLE owners (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 id text NOT NULL UNIQUE,
 email text NOT NULL UNIQUE CHECK (email = lower(email)),
 password_hash text NOT NULL,
 mfa_enabled boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY,
 owner_id text NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE auth_buckets (
 key_hash text PRIMARY KEY,
 window_started timestamptz NOT NULL,
 attempts integer NOT NULL CHECK (attempts > 0)
);
CREATE INDEX auth_buckets_expiry ON auth_buckets(window_started);
