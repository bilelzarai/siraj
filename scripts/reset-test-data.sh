#!/usr/bin/env bash
# Clear the accounts the smoke test creates, and leave everything else alone.
#
# Two rules this script exists to enforce:
#
# 1. Do NOT use `TRUNCATE users CASCADE`: questions.created_by references users,
#    and TRUNCATE CASCADE follows that edge regardless of ON DELETE SET NULL —
#    it silently wipes the entire question bank.
#
# 2. Do NOT delete every user. An earlier version ran `DELETE FROM users` with
#    no predicate, so running the smoke test destroyed real accounts sharing the
#    development database. Fixture accounts all use @example.com, which RFC 2606
#    reserves for exactly this and which no real address can ever match.
set -euo pipefail

CONTAINER=${DB_CONTAINER:-islamic-game-db}
DB_USER=${DB_USER:-islamic}
DB_NAME=${DB_NAME:-islamic_game}

docker exec -i "$CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;

-- These SET NULL instead of cascading, so the rows would outlive the accounts
-- that made them — deliberately, since a note stays readable after its author
-- leaves. Clear them while the author is still resolvable.
DELETE FROM admin_audit
 WHERE actor_id IN (SELECT id FROM users WHERE email LIKE '%@example.com');
DELETE FROM question_imports
 WHERE actor_id IN (SELECT id FROM users WHERE email LIKE '%@example.com');
DELETE FROM question_comments
 WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@example.com');

-- Everything a player owns cascades from users: sessions, rounds, answers,
-- duels, conversations, messages, notifications, badges, friendships and
-- support tickets all go with this one statement.
DELETE FROM users WHERE email LIKE '%@example.com';

-- Questions this script's own fixtures created, and nothing else.
--
-- 3. Do NOT match on the id. This used to delete everything with id >= 9000,
--    on the reasoning that fixtures pick ids up there and the real bank sat in
--    the low hundreds. That reasoning had an expiry date on it: a real import
--    numbers its rows however the source file does, and a bank imported with
--    ten-digit ids is entirely above 9000. Running the smoke suite then
--    destroyed the whole question bank, silently, at the start of a run whose
--    job was to test something unrelated.
--
--    The marker in the prompt is the only thing that actually says "a test
--    wrote this". Every fixture in smoke.sh carries one; nothing a person
--    authors does.
DELETE FROM questions q
 WHERE EXISTS (
   SELECT 1 FROM question_translations t
    WHERE t.question_id = q.id
      AND (t.prompt LIKE 'ZZ %' OR t.prompt LIKE 'Smoke test%')
 );

COMMIT;
SQL

# A blast radius this script has no business exceeding. Fixtures number in the
# handful; anything more means the matching rule has drifted again, and the
# next run should stop rather than take the bank with it.
REMAINING=$(docker exec -i "$CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc \
  "SELECT count(*) FROM questions")
echo "reset-test-data: fixtures cleared, ${REMAINING} questions remain" >&2
