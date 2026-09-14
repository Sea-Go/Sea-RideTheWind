-- H01 option 1 candidate. Apply explicitly to the RTW User Center database
-- before enabling account-link routes. Existing users and logins are unchanged.
CREATE TABLE IF NOT EXISTS rtw_dc_link_identities (
    dc_user_id uuid PRIMARY KEY,
    uid bigint NOT NULL CHECK (uid > 0),
    first_bound_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS rtw_dc_account_links (
    uid bigint PRIMARY KEY CHECK (uid > 0),
    dc_user_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    state text NOT NULL CHECK (state IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (dc_user_id) REFERENCES rtw_dc_link_identities(dc_user_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS rtw_dc_account_links_active_dc
    ON rtw_dc_account_links(dc_user_id) WHERE state = 'active';
