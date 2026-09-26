-- Version strings sort as numbers where they are numbers: 2.9, 2.10, 10.0
-- rather than 10.0, 2.10, 2.9. ICU provides this as a collation option.
--
-- A PostgreSQL built without ICU cannot create it. That is not a reason to
-- refuse to start, so the failure is swallowed and the server falls back to
-- plain text ordering when the collation is not there (content.Service
-- checks at start-up).
DO $$
BEGIN
  CREATE COLLATION IF NOT EXISTS numeric_text (provider = icu, locale = 'und-u-kn');
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'numeric_text collation unavailable (%), versions will sort as text', SQLERRM;
END $$;
