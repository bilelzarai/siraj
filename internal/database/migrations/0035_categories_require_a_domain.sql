-- =============================================================================
-- 0035_categories_require_a_domain.sql
--
-- 0034 gave categories.domain_id a default so that every writer kept working
-- between the schema and the screens. The category form names a domain now, so
-- the default has done its job — and past this point it would hide the bug it
-- was protecting against: a form that forgot the field would file a category
-- under whichever domain happens to be first, silently, instead of being
-- refused. A column that is required should say so.
-- =============================================================================

ALTER TABLE categories ALTER COLUMN domain_id DROP DEFAULT;
