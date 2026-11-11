-- =============================================================================
-- 0037_arabic_domain.sql — a second subject area: Arabic & Qur'anic sciences
--
-- 0034 built the level above a category and said the point of it out loud: a
-- second subject area should arrive without a deploy, because nothing in Go
-- knows any domain by name. This is the first one to arrive, and it is still a
-- migration rather than three hand-typed INSERTs on somebody's laptop — the
-- taxonomy a clean clone comes up with has to be the same taxonomy production
-- has, and the only way that stays true is if it is written down here.
--
-- Three categories, filed under it, and no questions. Questions come from a
-- source — a file somebody wrote and reviewed — not from whoever happened to be
-- editing this migration. The categories are empty until that import lands, and
-- an empty category is honest: the setup screen already counts what each one
-- can draw and disables what it cannot fill.
-- =============================================================================

INSERT INTO domains (id, slug, icon, color, sort_order)
VALUES (2, 'arabic', '🖋️', '#d946ef', 2)
ON CONFLICT (id) DO NOTHING;
SELECT setval('domains_id_seq', (SELECT max(id) FROM domains));

-- source 'seed' and needs_review false, the same as the Islamic domain in 0034.
-- Not a formality: the category and domain reads join ON ... AND NOT
-- needs_review, so a name marked for review is not shown at all — it does not
-- render in grey awaiting approval, it renders as nothing. A taxonomy row has
-- to be either live or absent.
INSERT INTO domain_translations (domain_id, locale, name, description, source) VALUES
  (2, 'ar', 'اللغة العربية وعلوم القرآن', 'اللغة والتجويد والتفسير',        'seed'),
  (2, 'en', 'Arabic & Qur''anic Sciences', 'Language, tajwīd and tafsīr',    'seed'),
  (2, 'fr', 'Langue arabe et sciences coraniques', 'Langue, tajwîd et tafsîr', 'seed')
ON CONFLICT (domain_id, locale) DO NOTHING;

-- Deliberately beside the Qur'an category rather than inside it. That one is
-- about the text — which surah, which verse, how many. These three are about
-- reading it: the language it is in, how it is pronounced, and what it means.
INSERT INTO categories (id, slug, icon, color, sort_order, domain_id)
VALUES ( 9, 'arabic-language', '🔤', '#0891b2',  9, 2),
       (10, 'tajwid',          '🎙️', '#ea580c', 10, 2),
       (11, 'tafsir',          '🔍', '#7c3aed', 11, 2)
ON CONFLICT (id) DO NOTHING;
SELECT setval('categories_id_seq', (SELECT max(id) FROM categories));

INSERT INTO category_translations (category_id, locale, name, description, source) VALUES
  ( 9, 'ar', 'اللغة العربية', 'النحو والصرف والمفردات',                      'seed'),
  ( 9, 'en', 'Arabic Language', 'Grammar, morphology and vocabulary',         'seed'),
  ( 9, 'fr', 'La langue arabe', 'Grammaire, morphologie et vocabulaire',      'seed'),
  (10, 'ar', 'التجويد', 'أحكام التلاوة ومخارج الحروف',                        'seed'),
  (10, 'en', 'Tajwīd', 'The rules of recitation and articulation',            'seed'),
  (10, 'fr', 'Le Tajwîd', 'Les règles de récitation et l''articulation',      'seed'),
  (11, 'ar', 'التفسير', 'معاني الآيات وأسباب النزول',                          'seed'),
  (11, 'en', 'Tafsīr', 'The meanings of verses and the occasions of revelation', 'seed'),
  (11, 'fr', 'Le Tafsîr', 'Le sens des versets et les causes de la révélation', 'seed')
ON CONFLICT (category_id, locale) DO NOTHING;
