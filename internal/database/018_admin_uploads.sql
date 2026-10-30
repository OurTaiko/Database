-- Only administrators upload charts now, as the account center says; the per-user
-- upload permission bit goes.
ALTER TABLE users DROP COLUMN permission_flags;
