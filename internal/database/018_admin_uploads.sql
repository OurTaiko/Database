-- Only administrators upload charts now, as the account center says; the per-user
-- upload permission bit goes. IF EXISTS: the first release of this file did not
-- record its version, so it runs again where the column is already gone.
ALTER TABLE users DROP COLUMN IF EXISTS permission_flags;

INSERT INTO internal.schema_migrations(version) VALUES (18);
