#!/usr/bin/env bash
# End-to-end smoke test against a running server.
set -uo pipefail

BASE=http://localhost:8080
REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DIR=${SMOKE_TMP:-$(mktemp -d)}

# A run must not outlive its own fixtures.
#
# reset-test-data.sh runs at the top of this script, which makes the suite
# repeatable and does nothing about what a finished run leaves behind. So every
# fixture stayed in the development database until somebody happened to run the
# suite again — and one of them, a question asking the colour of the fourth
# planet, sat active in the Manners & Ethics category, in English only, where a
# player could draw it in a real round.
#
# Cleaning up on EXIT rather than at the end of the happy path, because a run
# that fails half way leaves the most behind. SMOKE_KEEP=1 holds the fixtures
# for inspection.
cleanup() {
  code=$?
  [ -n "${SMOKE_TMP:-}" ] || rm -rf "$DIR"
  [ -n "${SMOKE_KEEP:-}" ] || "$REPO/scripts/reset-test-data.sh" >/dev/null 2>&1 || true
  exit $code
}
trap cleanup EXIT
A=$DIR/cookieA.txt
B=$DIR/cookieB.txt

# Three things here read the database — this script through psql, the server
# under test, and sirajctl — and they have to be the same database. Named once,
# here, because the one that was left implicit was sirajctl: it falls back to
# DATABASE_URL in .env, which holds whatever the last deploy was pointed at. So
# `promote` landed on another server, the account under test stayed a player,
# and seventy-six admin checks reported 404 without a word about why.
DB_CONTAINER=${DB_CONTAINER:-siraj-game-db}
DB_USER=${DB_USER:-siraj}
DB_NAME=${DB_NAME:-siraj-db}
DB_PASSWORD=${DB_PASSWORD:-siraj}
DB_PORT=${DB_PORT:-5434}
export DATABASE_URL=${SMOKE_DATABASE_URL:-postgres://$DB_USER:$DB_PASSWORD@localhost:$DB_PORT/$DB_NAME?sslmode=disable}

# Built from source rather than taken from bin/, which is ignored by git: on a
# fresh clone it does not exist, and on an old one it is whatever was last
# compiled. Either way the admin half of this suite was testing nothing.
CTL=$DIR/sirajctl
if ! (cd "$REPO" && go build -o "$CTL" ./cmd/sirajctl); then
  echo "cannot build cmd/sirajctl — the admin checks need it" >&2
  exit 1
fi
"$REPO/scripts/reset-test-data.sh" >/dev/null 2>&1 || true
rm -f "$A" "$B"

pass=0; fail=0
check() { # check <label> <expected> <actual>
  if [ "$2" = "$3" ]; then echo "  ok   $1 ($3)"; pass=$((pass+1));
  else echo "  FAIL $1: expected $2 got $3"; fail=$((fail+1)); fi
}
contains() { # contains <label> <needle> <file>
  if grep -qF -- "$2" "$3"; then echo "  ok   $1"; pass=$((pass+1));
  else echo "  FAIL $1: '$2' not found"; fail=$((fail+1)); fi
}

code() { curl -s -o "$2" -w '%{http_code}' "${@:3}"; }
csrf() { grep -oP 'siraj_csrf\s+\K\S+' "$1" | tail -1; }

echo "== the reset only touches fixture accounts =="
# This script used to run an unscoped DELETE FROM users, so running the smoke
# test destroyed real accounts sharing the development database. The canary
# below must still be standing afterwards.
docker exec -i "${DB_CONTAINER:-siraj-game-db}" psql -U "${DB_USER:-siraj}" \
  -d "${DB_NAME:-siraj-db}" -q -c \
  "INSERT INTO users (username, display_name, email, password_hash, avatar_seed, locale, role, status)
   VALUES ('smoke_canary','Canary','canary@not-example.test','\$2a\$10\$x','seed','en','player','active')
   ON CONFLICT (username) DO NOTHING;" >/dev/null 2>&1
"$REPO/scripts/reset-test-data.sh" >/dev/null 2>&1
SURVIVED=$(docker exec -i "${DB_CONTAINER:-siraj-game-db}" psql -U "${DB_USER:-siraj}" \
  -d "${DB_NAME:-siraj-db}" -tAc \
  "SELECT count(*) FROM users WHERE username = 'smoke_canary';" 2>/dev/null)
check "a non-fixture account survives the reset" 1 "$SURVIVED"
FIXTURES=$(docker exec -i "${DB_CONTAINER:-siraj-game-db}" psql -U "${DB_USER:-siraj}" \
  -d "${DB_NAME:-siraj-db}" -tAc \
  "SELECT count(*) FROM users WHERE email LIKE '%@example.com';" 2>/dev/null)
check "fixture accounts are cleared" 0 "$FIXTURES"
docker exec -i "${DB_CONTAINER:-siraj-game-db}" psql -U "${DB_USER:-siraj}" \
  -d "${DB_NAME:-siraj-db}" -q -c "DELETE FROM users WHERE username='smoke_canary';" >/dev/null 2>&1

echo

echo "== public pages =="
check "landing"   200 "$(code x $DIR/out.html $BASE/)"
contains "landing has tagline" "لعبة إسلامية" $DIR/out.html
check "landing en" 200 "$(code x $DIR/out.html "$BASE/?lang=en")"
contains "landing en copy" "blends fun with learning" $DIR/out.html
check "landing fr" 200 "$(code x $DIR/out.html "$BASE/?lang=fr")"
contains "landing fr copy" "plaisir et apprentissage" $DIR/out.html
# What the page links, not what it used to link. These two asked for fixed
# paths that the bundler replaced with hashed filenames; they would now pass
# only by accident, or fail for the wrong reason.
curl -s "$BASE/" -o $DIR/head.html
CSS_URL=$(grep -oP '<link rel="stylesheet" href="\K[^"]+' $DIR/head.html | head -1)
JS_URL=$(grep -oP '<script src="\K[^"]+(?=" type="module")' $DIR/head.html | head -1)
check "page links a stylesheet" "yes" "$([ -n "$CSS_URL" ] && echo yes || echo no)"
check "page links a script"     "yes" "$([ -n "$JS_URL" ] && echo yes || echo no)"
check "css"       200 "$(code x /dev/null "$BASE$CSS_URL")"
check "js"        200 "$(code x /dev/null "$BASE$JS_URL")"
check "healthz"   200 "$(code x /dev/null $BASE/healthz)"
check "404"       404 "$(code x /dev/null $BASE/nope)"
check "guard /app" 303 "$(code x /dev/null -o /dev/null $BASE/app)"

echo "== register =="
curl -s -c "$A" "$BASE/register?lang=en" -o $DIR/reg.html
TOKA=$(csrf "$A")
check "csrf issued" "yes" "$([ -n "$TOKA" ] && echo yes || echo no)"

check "register A" 303 "$(curl -s -b "$A" -c "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/register \
  -d "csrf_token=$TOKA" -d "display_name=Aisha Test" -d "username=aisha_t" \
  -d "email=aisha@example.com" -d "password=supersecret1" -d "password_confirm=supersecret1")"

curl -s -c "$B" "$BASE/register?lang=fr" -o /dev/null
TOKB=$(csrf "$B")
check "register B" 303 "$(curl -s -b "$B" -c "$B" -o /dev/null -w '%{http_code}' -X POST $BASE/register \
  -d "csrf_token=$TOKB" -d "display_name=Bilal Test" -d "username=bilal_t" \
  -d "email=bilal@example.com" -d "password=supersecret1" -d "password_confirm=supersecret1")"

# Everything below reads back what the server wrote, so a server pointed at
# another database turns the rest of this run into noise. Asking once, here,
# costs one query and replaces a page of unexplained failures.
SEEN=$(docker exec -i "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc \
  "SELECT count(*) FROM users WHERE username = 'aisha_t';" 2>/dev/null)
if [ "${SEEN:-0}" != "1" ]; then
  echo
  echo "  FAIL registration wrote nothing this script can see"
  echo "       Either the server is pointed at another database, or — far more"
  echo "       often — this suite has been run several times within the hour and"
  echo "       the register limiter (ten per address per hour) is now refusing."
  echo "       Restart the server to clear it; the limiter is in memory."
  echo "       start it with DATABASE_URL=$DATABASE_URL"
  exit 1
fi

echo "== validation =="
curl -s -c $DIR/cookieC.txt "$BASE/register?lang=en" -o /dev/null
TOKC=$(csrf $DIR/cookieC.txt)
check "dup username" 422 "$(curl -s -b $DIR/cookieC.txt -o $DIR/dup.html -w '%{http_code}' -X POST $BASE/register \
  -d "csrf_token=$TOKC" -d "display_name=Copy Cat" -d "username=aisha_t" \
  -d "email=other@example.com" -d "password=supersecret1" -d "password_confirm=supersecret1")"
contains "dup username message" "already taken" $DIR/dup.html
check "short password" 422 "$(curl -s -b $DIR/cookieC.txt -o $DIR/short.html -w '%{http_code}' -X POST $BASE/register \
  -d "csrf_token=$TOKC" -d "display_name=Short Pass" -d "username=shorty" \
  -d "email=short@example.com" -d "password=abc" -d "password_confirm=abc")"
contains "short password message" "at least 8" $DIR/short.html
check "csrf rejected" 303 "$(curl -s -b $DIR/cookieC.txt -o /dev/null -w '%{http_code}' -X POST $BASE/register \
  -d "csrf_token=bogus" -d "display_name=No CSRF" -d "username=nocsrf" \
  -d "email=n@example.com" -d "password=supersecret1" -d "password_confirm=supersecret1")"

echo "== dashboard =="
check "dashboard A" 200 "$(code x $DIR/dash.html -b "$A" $BASE/app)"
contains "greeting" "Aisha Test" $DIR/dash.html
contains "category rendered" "The Noble Qur" $DIR/dash.html
check "dashboard B (fr)" 200 "$(code x $DIR/dashb.html -b "$B" $BASE/app)"
contains "fr category" "Le Saint Coran" $DIR/dashb.html

echo "== friends =="
TOKA=$(csrf "$A")
check "A requests B" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/friends/request \
  -d "csrf_token=$TOKA" -d "username=bilal_t")"
check "B sees request" 200 "$(code x $DIR/req.html -b "$B" "$BASE/friends?tab=requests")"
contains "request listed" "Aisha Test" $DIR/req.html
TOKB=$(csrf "$B")
check "B accepts" 303 "$(curl -s -b "$B" -o /dev/null -w '%{http_code}' -X POST $BASE/friends/accept \
  -d "csrf_token=$TOKB" -d "username=aisha_t")"
check "A friends list" 200 "$(code x $DIR/fr.html -b "$A" "$BASE/friends?tab=all")"
contains "friend listed" "Bilal Test" $DIR/fr.html
check "search" 200 "$(code x $DIR/search.html -b "$A" "$BASE/friends?tab=search&q=bilal")"
contains "search hit" "bilal_t" $DIR/search.html

echo "== play a solo round =="
TOKA=$(csrf "$A")
check "start round" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/play/start \
  -d "csrf_token=$TOKA" -d "category=1" -d "difficulty=0" -d "count=5")"
check "round page" 200 "$(code x $DIR/round.html -b "$A" $BASE/play/round)"
contains "question rendered" 'data-round' $DIR/round.html

# Leaving a round used to let its clock run on, so the dashboard's "resume"
# handed back a question that had already expired. The page banks its elapsed
# time as it goes away; resuming has to pick up from there, not from the wall
# clock and not from zero.
echo "== the clock stops when the page does =="
elapsed() { grep -oP 'data-elapsed="\K[0-9]+' "$1"; }
sleep 2
check "bank the clock" 204 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/play/pause \
  --data-urlencode "csrf_token=$(csrf "$A")" --data-urlencode "position=0")"
sleep 2
code x $DIR/resumed.html -b "$A" $BASE/play/round > /dev/null
RESUMED=$(elapsed $DIR/resumed.html)
check "resumes where it was left" "yes" \
  "$([ "$RESUMED" -ge 1500 ] && [ "$RESUMED" -lt 3500 ] && echo yes || echo "no (${RESUMED}ms)")"
# A beacon can land after the answer it was racing. Banking it against the next
# question would charge the player for time spent on the previous one.
check "a stale bank is ignored" 204 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/play/pause \
  --data-urlencode "csrf_token=$(csrf "$A")" --data-urlencode "position=4")"
code x $DIR/unmoved.html -b "$A" $BASE/play/round > /dev/null
check "and changes nothing" "yes" \
  "$([ "$(elapsed $DIR/unmoved.html)" -ge "$RESUMED" ] && echo yes || echo no)"

for i in 0 1 2 3 4; do
  RES=$(curl -s -b "$A" -X POST $BASE/play/answer \
    -H 'Content-Type: application/json' -H 'Accept: application/json' \
    -H "X-CSRF-Token: $(csrf "$A")" \
    -d "{\"position\":$i,\"choice\":0,\"timeMs\":2500}")
  echo "    answer $i -> $RES"
  if [ $i -lt 4 ]; then curl -s -b "$A" -o /dev/null $BASE/play/round; fi
done

check "replay rejected" 409 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/play/answer \
  -H 'Content-Type: application/json' -H 'Accept: application/json' \
  -H "X-CSRF-Token: $(csrf "$A")" -d '{"position":0,"choice":0,"timeMs":100}')"

check "finish" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/play/finish -d "csrf_token=$(csrf "$A")")"
RESULT=$(curl -s -b "$A" -o /dev/null -w '%{redirect_url}' -X POST $BASE/play/finish -d "csrf_token=$(csrf "$A")")
check "history list" 200 "$(code x $DIR/hist.html -b "$A" $BASE/history)"
contains "history has a round" "history-row" $DIR/hist.html
SESSION_ID=$(grep -oP '/history/\K[0-9a-f-]{36}' $DIR/hist.html | head -1)
check "review page" 200 "$(code x $DIR/review.html -b "$A" $BASE/history/$SESSION_ID)"
contains "review shows explanation" "review-item" $DIR/review.html
check "other user cannot review" 403 "$(code x /dev/null -b "$B" $BASE/history/$SESSION_ID)"

echo "== challenge =="
TOKA=$(csrf "$A")
check "create challenge" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/challenges/new \
  -d "csrf_token=$TOKA" -d "opponent=bilal_t" -d "category=2" -d "difficulty=0" -d "count=5" -d "message=Bonne chance")"
check "B sees challenge" 200 "$(code x $DIR/ch.html -b "$B" "$BASE/challenges?tab=incoming")"
contains "challenge listed" "Aisha Test" $DIR/ch.html
CH_ID=$(grep -oP '/challenges/\K[0-9a-f-]{36}' $DIR/ch.html | head -1)
# Accepting joins; it no longer opens a round. The host sets the match going
# with one press on /start, which opens a round for everybody who accepted at
# the same moment — their own included. Driving it the old way (a second accept
# carrying confirm=1) still answers 303, so this flow has to assert the state
# it produces and not only the status code.
check "B joins the match" 303 "$(curl -s -b "$B" -o /dev/null -w '%{http_code}' -X POST $BASE/challenges/$CH_ID/accept -d "csrf_token=$(csrf "$B")")"
check "the host starts the match" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/challenges/$CH_ID/start -d "csrf_token=$(csrf "$A")")"
check "B has a round to play" 200 "$(code x /dev/null -b "$B" $BASE/play/round)"

for i in 0 1 2 3 4; do
  curl -s -b "$B" -X POST $BASE/play/answer -H 'Content-Type: application/json' -H 'Accept: application/json' \
    -H "X-CSRF-Token: $(csrf "$B")" -d "{\"position\":$i,\"choice\":1,\"timeMs\":3000}" > /dev/null
  curl -s -b "$B" -o /dev/null $BASE/play/round
done
curl -s -b "$B" -o /dev/null -X POST $BASE/play/finish -d "csrf_token=$(csrf "$B")"

# The host's round opened with the press that started the match, so they play
# straight away rather than accepting their own challenge.
check "A has a round to play" 200 "$(code x /dev/null -b "$A" $BASE/play/round)"
for i in 0 1 2 3 4; do
  curl -s -b "$A" -X POST $BASE/play/answer -H 'Content-Type: application/json' -H 'Accept: application/json' \
    -H "X-CSRF-Token: $(csrf "$A")" -d "{\"position\":$i,\"choice\":2,\"timeMs\":1500}" > /dev/null
  curl -s -b "$A" -o /dev/null $BASE/play/round
done
curl -s -b "$A" -o /dev/null -X POST $BASE/play/finish -d "csrf_token=$(csrf "$A")"
check "finished duels" 200 "$(code x $DIR/chf.html -b "$A" "$BASE/challenges?tab=finished")"
# Two players render the face-off; three or more render the standing list.
contains "duel scoreboard" "duel__score" $DIR/chf.html

echo "== messages =="
check "open thread" 303 "$(code x /dev/null -b "$A" $BASE/messages/with/bilal_t)"
CONV=$(curl -s -b "$A" -o /dev/null -w '%{redirect_url}' $BASE/messages/with/bilal_t | grep -oP '[0-9a-f-]{36}')
check "thread page" 200 "$(code x $DIR/msg.html -b "$A" $BASE/messages/$CONV)"
SEND=$(curl -s -b "$A" -X POST $BASE/messages/$CONV -H 'Accept: application/json' -H 'X-Requested-With: fetch' \
  -H "X-CSRF-Token: $(csrf "$A")" -d "csrf_token=$(csrf "$A")" -d "body=Assalamu alaykum ya Bilal")
echo "    send -> $SEND"
check "B inbox" 200 "$(code x $DIR/inbox.html -b "$B" $BASE/messages)"
contains "message preview" "Assalamu alaykum" $DIR/inbox.html
POLL=$(curl -s -b "$B" -H 'Accept: application/json' "$BASE/messages/$CONV/poll?after=0")
echo "    poll -> $POLL"
check "counts api" 200 "$(code x /dev/null -b "$B" -H 'Accept: application/json' $BASE/ui/counts)"

echo "== profile / leaderboard / settings =="
check "own profile" 200 "$(code x $DIR/prof.html -b "$A" -L $BASE/profile)"
contains "badges section" "badge-tile" $DIR/prof.html
contains "category breakdown" "spark__col" $DIR/prof.html
check "other profile" 200 "$(code x $DIR/prof2.html -b "$A" $BASE/u/bilal_t)"
contains "friend actions" "challenges/new?opponent=bilal_t" $DIR/prof2.html
check "leaderboard" 200 "$(code x $DIR/lb.html -b "$A" $BASE/leaderboard)"
contains "leaderboard rows" "lb-row" $DIR/lb.html
check "leaderboard friends" 200 "$(code x /dev/null -b "$A" "$BASE/leaderboard?scope=friends")"
check "settings" 200 "$(code x $DIR/set.html -b "$A" $BASE/settings)"
contains "sessions listed" "Active sessions" $DIR/set.html
check "edit profile form" 200 "$(code x /dev/null -b "$A" $BASE/profile/edit)"
check "save profile" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/profile/edit \
  -d "csrf_token=$(csrf "$A")" -d "display_name=Aisha Updated" -d "bio=Learning every day" -d "country=Tunisia")"
check "profile shows bio" 200 "$(code x $DIR/prof3.html -b "$A" -L $BASE/profile)"
contains "updated bio" "Learning every day" $DIR/prof3.html

echo "== locale switch =="
TOKA=$(csrf "$A")
check "switch to ar" 303 "$(curl -s -b "$A" -c "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/settings/locale \
  -d "csrf_token=$TOKA" -d "locale=ar" -d "redirect=/app")"
check "dashboard in ar" 200 "$(code x $DIR/dashar.html -b "$A" $BASE/app)"
contains "arabic greeting" "السلام عليكم" $DIR/dashar.html
contains "rtl direction" 'dir="rtl"' $DIR/dashar.html
check "play page ar" 200 "$(code x $DIR/playar.html -b "$A" $BASE/play)"
contains "arabic category" "القرآن الكريم" $DIR/playar.html

echo "== password + logout =="
check "change password" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/settings/password \
  -d "csrf_token=$(csrf "$A")" -d "current_password=supersecret1" -d "new_password=evenbettersecret2")"
check "logout" 303 "$(curl -s -b "$A" -c "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/logout -d "csrf_token=$(csrf "$A")")"
check "after logout /app redirects" 303 "$(code x /dev/null -b "$A" $BASE/app)"
curl -s -c $DIR/cookieD.txt "$BASE/login" -o /dev/null
check "login with new password" 303 "$(curl -s -b $DIR/cookieD.txt -c $DIR/cookieD.txt -o /dev/null -w '%{http_code}' -X POST $BASE/login \
  -d "csrf_token=$(csrf $DIR/cookieD.txt)" -d "identifier=aisha_t" -d "password=evenbettersecret2")"
rm -f $DIR/cookieE.txt; curl -s -c $DIR/cookieE.txt "$BASE/login" -o /dev/null
check "login with old password fails" 401 "$(curl -s -b $DIR/cookieE.txt -o $DIR/badlogin.html -w '%{http_code}' -X POST $BASE/login \
  -d "csrf_token=$(csrf $DIR/cookieE.txt)" -d "identifier=aisha_t" -d "password=supersecret1")"
contains "bad login message" "لا تطابق أي حساب" $DIR/badlogin.html


# The section above signs jar A out, and everything below needs a session.
# Sign back in with the rotated password.
rm -f "$A"; curl -s -c "$A" "$BASE/login?lang=en" -o /dev/null
curl -s -b "$A" -c "$A" -o /dev/null -X POST $BASE/login \
  -d "csrf_token=$(csrf "$A")" -d "identifier=aisha_t" -d "password=evenbettersecret2"
check "re-authenticated for the admin suite" 200 "$(code x /dev/null -b "$A" $BASE/app)"

echo "== back navigation (every non-tab screen) =="
for path in /settings /profile/edit /history /leaderboard /friends /challenges /play; do
  page=$DIR/back$(echo "$path" | tr '/' '_').html
  curl -s -b "$A" "$BASE$path" -o "$page"
  if grep -q 'class="backlink"' "$page"; then
    echo "  ok   back link on $path"; pass=$((pass+1))
  else
    echo "  FAIL no back link on $path"; fail=$((fail+1))
  fi
done

echo "== asset cache-busting =="
# A fingerprint in the filename, which is what lets the response be cached
# forever. Hashed by the bundler now rather than appended as ?v=, so the check
# is that the name carries one — not which one.
curl -s -b "$A" "$BASE/app" -o $DIR/assets.html
APP_CSS=$(grep -oP '<link rel="stylesheet" href="\K[^"]+' $DIR/assets.html | head -1)
APP_JS=$(grep -oP '<script src="\K[^"]+(?=" type="module")' $DIR/assets.html | head -1)
check "css url carries a fingerprint" "yes" \
  "$(echo "$APP_CSS" | grep -qE '/static/dist/.*-[A-Za-z0-9_-]{8,}\.css$' && echo yes || echo no)"
check "js url carries a fingerprint" "yes" \
  "$(echo "$APP_JS" | grep -qE '/static/dist/.*-[A-Za-z0-9_-]{8,}\.js$' && echo yes || echo no)"
check "the signed-in page links the same build" "yes" \
  "$([ "$APP_CSS" = "$CSS_URL" ] && echo yes || echo no)"

echo "== confirm-answer button =="
check "start round" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/play/start \
  -d "csrf_token=$(csrf "$A")" -d "category=1" -d "difficulty=0" -d "count=5")"
check "round page" 200 "$(code x $DIR/round2.html -b "$A" $BASE/play/round)"
contains "confirm button present" "data-confirm-answer" $DIR/round2.html
contains "confirm starts disabled" "data-confirm-answer disabled" $DIR/round2.html
contains "answers are radio-like" 'aria-checked="false"' $DIR/round2.html
curl -s -b "$A" -o /dev/null -X POST $BASE/play/quit -d "csrf_token=$(csrf "$A")"

echo "== support: player side =="
check "support list" 200 "$(code x /dev/null -b "$A" $BASE/support)"
check "support form" 200 "$(code x /dev/null -b "$A" $BASE/support/new)"
check "reject empty kind" 422 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/support/new \
  -d "csrf_token=$(csrf "$A")" -d "subject=Hello there" -d "body=This message has no kind at all.")"
check "create ticket" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/support/new \
  -d "csrf_token=$(csrf "$A")" -d "kind=suggestion" -d "subject=Please add more Hadith questions" \
  -d "body=I would really enjoy more questions from Sahih Muslim.")"
check "ticket listed" 200 "$(code x $DIR/sup.html -b "$A" $BASE/support)"
contains "ticket visible" "Please add more Hadith questions" $DIR/sup.html

echo "== admin gating =="
check "player cannot see /admin" 404 "$(code x /dev/null -b "$A" $BASE/admin)"
check "player cannot see /admin/users" 404 "$(code x /dev/null -b "$A" $BASE/admin/users)"
check "player cannot see /admin/support" 404 "$(code x /dev/null -b "$A" $BASE/admin/support)"

echo "== admin area (promoted account) =="
"$CTL" promote aisha_t > /dev/null 2>&1
for path in /admin /admin/users /admin/users/new /admin/support /admin/questions \
            /admin/questions/import /admin/review /admin/integrity /admin/audit; do
  check "admin $path" 200 "$(code x /dev/null -b "$A" "$BASE$path")"
done
check "csv template" 200 "$(code x $DIR/tmpl.csv -b "$A" $BASE/admin/questions/import/template.csv)"
contains "template header" "id,category,difficulty" $DIR/tmpl.csv

echo "== admin creates a user with a role =="
check "create moderator" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/admin/users/new \
  -d "csrf_token=$(csrf "$A")" -d "display_name=Mod Person" -d "username=mod_person" \
  -d "email=mod@example.com" -d "password=modsecret123" -d "role=moderator")"
check "duplicate username rejected" 422 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/admin/users/new \
  -d "csrf_token=$(csrf "$A")" -d "display_name=Clash" -d "username=mod_person" \
  -d "email=other@example.com" -d "password=modsecret123" -d "role=player")"
check "users list shows them" 200 "$(code x $DIR/users.html -b "$A" "$BASE/admin/users?q=mod_person")"
contains "moderator listed" "mod_person" $DIR/users.html

echo "== admin cannot demote the last admin or act on self =="
check "self-action blocked" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST \
  "$BASE/admin/users/$("$CTL" whois aisha_t | grep -oP '^id:\s+\K\S+')/suspend" -d "csrf_token=$(csrf "$A")")"

echo "== bulk import: preview writes nothing, commit writes =="
cat > $DIR/imp.json <<'JSON'
[{"id":9201,"category":"akhlaq","difficulty":1,"correct":0,
  "t":{"en":{"prompt":"ZZ smoke fixture: which colour is the fourth planet commonly called?","choices":["Red","Blue","Green","Yellow"],"explanation":"Fixture row, deliberately unlike any seeded question."}}},
 {"id":9202,"category":"bogus_category","difficulty":1,"correct":0,
  "t":{"en":{"prompt":"Smoke test: bad category","choices":["a","b","c","d"],"explanation":"x"}}}]
JSON
BEFORE_Q=$("$CTL" stats | grep -oP '^questions:\s+\K\d+')
check "import preview" 200 "$(curl -s -b "$A" -o $DIR/prev.html -w '%{http_code}' -X POST $BASE/admin/questions/import \
  -F "csrf_token=$(csrf "$A")" -F "commit=0" -F "file=@$DIR/imp.json")"
contains "preview flags the bad category" "unknown category" $DIR/prev.html
MID_Q=$("$CTL" stats | grep -oP '^questions:\s+\K\d+')
check "preview wrote nothing" "$BEFORE_Q" "$MID_Q"
# Applying is one press on a file the server already holds from the preview.
# It used to mean attaching the same file a second time, which asked the admin
# to find it again and let them pick a different one by mistake.
STASH=$(grep -oP 'name="stash" value="\K[0-9a-f]+' $DIR/prev.html)
check "preview holds the upload" "yes" "$([ -n "$STASH" ] && echo yes || echo no)"
contains "apply asks for no second file" "$STASH" $DIR/prev.html

# A commit with no token behind it never had a preview.
check "a commit with no token" 200 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/admin/questions/import \
  -F "csrf_token=$(csrf "$A")" -F "commit=1" -F "file=@$DIR/imp.json")"
BARE_Q=$("$CTL" stats | grep -oP '^questions:\s+\K\d+')
check "and wrote nothing" "$BEFORE_Q" "$BARE_Q"

check "import commit" 200 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/admin/questions/import \
  -d "csrf_token=$(csrf "$A")" -d "commit=1" -d "stash=$STASH")"
AFTER_Q=$("$CTL" stats | grep -oP '^questions:\s+\K\d+')
check "commit added exactly one" "$((BEFORE_Q+1))" "$AFTER_Q"

echo "== support: staff triage =="
check "staff inbox" 200 "$(code x $DIR/inbox.html -b "$A" $BASE/admin/support)"
contains "player ticket in inbox" "Please add more Hadith questions" $DIR/inbox.html
TID=$(grep -oP '/admin/support/\K[0-9a-f-]{36}' $DIR/inbox.html | head -1)
check "staff thread" 200 "$(code x /dev/null -b "$A" "$BASE/admin/support/$TID")"
for f in "kind=suggestion" "status=open" "priority=normal" "waiting=1" "assignee=unassigned"; do
  check "filter $f" 200 "$(code x /dev/null -b "$A" "$BASE/admin/support?$f")"
done
check "internal note" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST "$BASE/admin/support/$TID/reply" \
  -d "csrf_token=$(csrf "$A")" -d "internal=1" -d "body=SECRETNOTE internal only")"
check "staff reply" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST "$BASE/admin/support/$TID/reply" \
  -d "csrf_token=$(csrf "$A")" -d "body=PUBLICREPLY thank you for writing in.")"
code x $DIR/staffview.html -b "$A" "$BASE/admin/support/$TID" > /dev/null
contains "staff sees the note" "SECRETNOTE" $DIR/staffview.html
code x $DIR/playerview.html -b "$A" "$BASE/support/$TID" > /dev/null
contains "player sees the reply" "PUBLICREPLY" $DIR/playerview.html
if grep -q 'SECRETNOTE' $DIR/playerview.html; then
  echo "  FAIL internal note leaked to the player"; fail=$((fail+1))
else
  echo "  ok   internal note stays internal"; pass=$((pass+1))
fi
check "triage update" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST "$BASE/admin/support/$TID/update" \
  -d "csrf_token=$(csrf "$A")" -d "status=resolved" -d "priority=high")"

echo "== audit trail recorded it all =="
check "audit page" 200 "$(code x $DIR/audit.html -b "$A" $BASE/admin/audit)"
contains "user.create audited" "user.create" $DIR/audit.html
contains "question.import audited" "question.import" $DIR/audit.html
contains "support.note audited" "support.note" $DIR/audit.html

echo "== forms name the exact field that failed =="
# The old behaviour was a single banner saying "something is wrong". Every
# check here asserts the failing field is identifiable, not just that the
# submission was rejected.
rm -f $DIR/cookieF.txt
curl -s -c $DIR/cookieF.txt "$BASE/register?lang=en" -o /dev/null
curl -s -b $DIR/cookieF.txt -c $DIR/cookieF.txt -o $DIR/badreg.html -X POST $BASE/register \
  -d "csrf_token=$(csrf $DIR/cookieF.txt)" -d "username=zz_field_probe" -d "email=not-an-email" \
  -d "display_name=Field Probe" -d "password=supersecret1" -d "password_confirm=supersecret1" >/dev/null
contains "register: banner asks for a correction" "Please correct the highlighted fields" $DIR/badreg.html
contains "register: field marked invalid" 'aria-invalid="true"' $DIR/badreg.html
contains "register: message tied to the input" 'id="email-error"' $DIR/badreg.html

curl -s -b $DIR/cookieF.txt -c $DIR/cookieF.txt -o $DIR/badreg2.html -X POST $BASE/register \
  -d "csrf_token=$(csrf $DIR/cookieF.txt)" -d "username=zz_field_probe" -d "email=probe@example.com" \
  -d "display_name=Field Probe" -d "password=supersecret1" -d "password_confirm=mismatch" >/dev/null
contains "register: mismatch marks password_confirm" 'id="password_confirm-error"' $DIR/badreg2.html

# Four faults must come back in one pass, not one per submit.
curl -s -b $DIR/cookieF.txt -c $DIR/cookieF.txt -o $DIR/badreg3.html -X POST $BASE/register \
  -d "csrf_token=$(csrf $DIR/cookieF.txt)" -d "username=ab" -d "email=bad-email" \
  -d "display_name=Q" -d "password=short" -d "password_confirm=short" >/dev/null
N=$(grep -o 'id="[a-z_]*-error"' $DIR/badreg3.html | sort -u | wc -l)
check "register: all four faults at once" 4 "$N"
if grep -q 'class="form-problems"' $DIR/badreg3.html; then
  echo "  FAIL banner still repeats the field list"; fail=$((fail+1))
else
  echo "  ok   banner summarises without repeating the fields"; pass=$((pass+1))
fi

# A failed login must mark both credential fields and say which neither, or it
# becomes an account-enumeration oracle.
rm -f $DIR/cookieG.txt
curl -s -c $DIR/cookieG.txt "$BASE/login?lang=en" -o /dev/null
curl -s -b $DIR/cookieG.txt -c $DIR/cookieG.txt -o $DIR/badlogin.html -X POST $BASE/login \
  -d "csrf_token=$(csrf $DIR/cookieG.txt)" -d "identifier=aisha_t" -d "password=definitely-wrong" >/dev/null
contains "login: identifier marked" 'id="identifier-error"' $DIR/badlogin.html
contains "login: password marked" 'id="password-error"' $DIR/badlogin.html
if grep -qiE 'no such (user|account)|user not found|unknown username|wrong password for' $DIR/badlogin.html; then
  echo "  FAIL login leaks which credential was wrong"; fail=$((fail+1))
else
  echo "  ok   login does not say which credential was wrong"; pass=$((pass+1))
fi

# Password change knows the difference between the two boxes.
check "settings: wrong current password" 422 "$(curl -s -b "$B" -o $DIR/badpw.html -w '%{http_code}' \
  -X POST $BASE/settings/password -d "csrf_token=$(csrf "$B")" \
  -d "current_password=not-the-one" -d "new_password=brandnewsecret9")"
contains "settings: marks current_password" 'id="current_password-error"' $DIR/badpw.html

# Support intake reports every problem at once rather than one per round trip.
check "support: empty ticket rejected" 422 "$(curl -s -b "$B" -o $DIR/badticket.html -w '%{http_code}' \
  -X POST $BASE/support/new -d "csrf_token=$(csrf "$B")" -d "kind=" -d "subject=x" -d "body=y")"
contains "support: marks kind" 'id="kind-error"' $DIR/badticket.html
contains "support: marks subject" 'id="subject-error"' $DIR/badticket.html
contains "support: marks body" 'id="body-error"' $DIR/badticket.html

echo "== the appearance button says which theme is on =="
for L in en fr ar; do
  curl -s -o $DIR/theme_$L.html "$BASE/login?lang=$L"
done
contains "en names applied + next" "Appearance: System · Switch to Light" $DIR/theme_en.html
contains "fr is translated" "Apparence : Système · Passer à Clair" $DIR/theme_fr.html
contains "ar is translated" "المظهر: حسب النظام · التبديل إلى فاتح" $DIR/theme_ar.html
if grep -q 'data-theme-toggle' $DIR/theme_en.html && ! grep -q 'aria-label="Appearance"' $DIR/theme_en.html; then
  echo "  ok   generic \"Appearance\" tooltip is gone"; pass=$((pass+1))
else
  echo "  FAIL generic Appearance tooltip still present"; fail=$((fail+1))
fi
curl -s -H "Cookie: theme=dark" -o $DIR/theme_dark.html "$BASE/login?lang=en"
contains "dark: shows the moon slot only" '<span class="theme-icon" data-theme-slot="dark">' $DIR/theme_dark.html
contains "dark: names the applied theme" "Appearance: Dark" $DIR/theme_dark.html
contains "dark: server preference survives boot" 'data-theme="dark"' $DIR/theme_dark.html
# The boot script used to clear data-theme whenever localStorage was empty,
# which threw away a signed-in user's saved theme on any fresh browser.
BOOT_URL=$(grep -oP '<script src="\K[^"]*boot\.js[^"]*' $DIR/theme_dark.html | head -1)
code x $DIR/boot.js "$BASE$BOOT_URL" > /dev/null
contains "boot adopts the server theme" "localStorage.setItem(\"theme\"" $DIR/boot.js
contains "boot script is a file, not inline" 'boot.js' $DIR/theme_dark.html
check "boot script is served" 200 "$(code x /dev/null "$BASE$BOOT_URL")"
# Copied verbatim rather than bundled, because a built entry is a module and a
# module script is deferred — which is the flash this script exists to prevent.
check "boot script is not a module" "yes" \
  "$(grep -q 'boot\.js[^>]*type="module"' $DIR/theme_dark.html && echo no || echo yes)"
# The play screen uses the Bare layout; it has to load the same boot script, or
# the theme flashes on the one screen a player spends the most time on.
curl -s -b "$A" -o /dev/null -X POST $BASE/play/start \
  -d "csrf_token=$(csrf "$A")" -d "difficulty=0" -d "count=5" >/dev/null
curl -s -b "$A" -o $DIR/playround.html $BASE/play/round
contains "play screen shares the boot script" "$BOOT_URL" $DIR/playround.html
curl -s -b "$A" -o /dev/null -X POST $BASE/play/quit -d "csrf_token=$(csrf "$A")"

echo "== icon-only controls carry a translated name =="
contains "language button names the language" 'aria-label="Language: English"' $DIR/theme_en.html
contains "language button in fr" 'aria-label="Langue : Français"' $DIR/theme_fr.html
contains "language button in ar" 'aria-label="اللغة: العربية"' $DIR/theme_ar.html
contains "reveal button is svg, not emoji" 'data-reveal-slot="show"' $DIR/theme_en.html
if grep -qE '🙈|👁️' $DIR/theme_en.html; then
  echo "  FAIL emoji still used for the reveal control"; fail=$((fail+1))
else
  echo "  ok   no emoji in the reveal control"; pass=$((pass+1))
fi

echo
echo "== forgotten password =="
# The compose stack's own catcher, on 8026 rather than the usual 8025 — a
# developer's machine often already has something there, and reading another
# project's mailbox is how this section reported five failures that meant
# "nothing was sent" rather than "the wrong thing was sent".
MH=${MAIL_URL:-${MAILHOG_URL:-http://localhost:8026}}
if curl -s -o /dev/null --max-time 2 "$MH/api/v1/messages"; then
  curl -s -X DELETE "$MH/api/v1/messages" >/dev/null
  rm -f $DIR/cookieR.txt
  curl -s -c $DIR/cookieR.txt "$BASE/forgot?lang=en" -o $DIR/forgot.html
  check "forgot form" 200 "$(code x /dev/null "$BASE/forgot")"
  curl -s -o $DIR/loginpage.html "$BASE/login?lang=en"
  contains "login offers the link" 'href="/forgot"' $DIR/loginpage.html

  # An address with no account must produce the same page and no mail, or the
  # form becomes a way to test whether any given address is registered.
  # The limiter is per-process and per-client, so running this suite several
  # times inside its window trips it. Detect that on the first request and skip
  # the section with a visible note, rather than reporting failures that really
  # mean "you have tested a lot lately". Restart the server to clear it.
  RC=$(curl -s -b $DIR/cookieR.txt -c $DIR/cookieR.txt -o $DIR/f1.html -w '%{http_code}' \
    -X POST $BASE/forgot -d "csrf_token=$(csrf $DIR/cookieR.txt)" -d "email=nobody@nowhere.invalid")
  if [ "$RC" = "429" ]; then
    echo "  SKIP reset flow: this client is rate-limited (restart the server to clear)"
    SKIP_RESET=1
  else
    SKIP_RESET=0
    contains "unknown address: same answer" "reset link is on its way" $DIR/f1.html
    UNKNOWN=$(curl -s "$MH/api/v1/messages" | grep -o '"total":[0-9]*' | head -1 | cut -d: -f2)
    check "unknown address: no mail sent" 0 "$UNKNOWN"
  fi

  if [ "$SKIP_RESET" = "0" ]; then
    curl -s -b $DIR/cookieR.txt -c $DIR/cookieR.txt -o $DIR/f2.html \
      -X POST $BASE/forgot -d "csrf_token=$(csrf $DIR/cookieR.txt)" -d "email=bilal@example.com" >/dev/null
    contains "known address: same answer" "reset link is on its way" $DIR/f2.html
  fi
  sleep 2
if [ "$SKIP_RESET" = "0" ]; then
    TOKEN=$("$REPO/scripts/mail-token.py" "$MH" 2>$DIR/mail.err || true)
    [ -n "$TOKEN" ] || echo "       $(cat $DIR/mail.err)"
    check "reset link arrived" "yes" "$([ -n "$TOKEN" ] && echo yes || echo no)"
    check "link opens the form" 200 "$(code x /dev/null "$BASE/reset?token=$TOKEN")"
    check "a bogus token is refused" 410 "$(code x /dev/null "$BASE/reset?token=not-a-real-token")"

    rm -f $DIR/cookieS.txt
    curl -s -c $DIR/cookieS.txt "$BASE/reset?token=$TOKEN&lang=en" -o /dev/null
    curl -s -b $DIR/cookieS.txt -c $DIR/cookieS.txt -o $DIR/badreset.html -X POST $BASE/reset \
      -d "csrf_token=$(csrf $DIR/cookieS.txt)" -d "token=$TOKEN" \
      -d "password=brandnewpass1" -d "password_confirm=nope" >/dev/null
    contains "mismatch marks the field" 'id="password_confirm-error"' $DIR/badreset.html

    check "redeem" 303 "$(curl -s -b $DIR/cookieS.txt -o /dev/null -w '%{http_code}' -X POST $BASE/reset \
      -d "csrf_token=$(csrf $DIR/cookieS.txt)" -d "token=$TOKEN" \
      -d "password=brandnewpass1" -d "password_confirm=brandnewpass1")"
    rm -f $DIR/cookieT.txt
    curl -s -c $DIR/cookieT.txt "$BASE/login?lang=en" -o /dev/null
    check "new password works" 303 "$(curl -s -b $DIR/cookieT.txt -o /dev/null -w '%{http_code}' -X POST $BASE/login \
      -d "csrf_token=$(csrf $DIR/cookieT.txt)" -d "identifier=bilal_t" -d "password=brandnewpass1")"
    check "the token cannot be spent twice" 410 "$(code x /dev/null "$BASE/reset?token=$TOKEN")"
    rm -f $DIR/cookieU.txt
    curl -s -c $DIR/cookieU.txt "$BASE/login?lang=en" -o /dev/null
    check "the old password is dead" 401 "$(curl -s -b $DIR/cookieU.txt -o /dev/null -w '%{http_code}' -X POST $BASE/login \
      -d "csrf_token=$(csrf $DIR/cookieU.txt)" -d "identifier=bilal_t" -d "password=supersecret1")"
    # Put the fixture back, or every later run starts with a password nobody
    # reading this script would expect.
    printf 'supersecret1' | "$CTL" passwd bilal_t >/dev/null
fi
else
  echo "  --   MailHog not reachable at $MH; skipping the reset flow"
fi

echo
echo "== notes on a question =="
# A note belongs to a question the player has actually answered. The endpoint
# takes a position in the live round, never a question id, so it cannot be
# used to ask about a question before being served it.
rm -f $DIR/cookieN.txt
curl -s -c $DIR/cookieN.txt "$BASE/login?lang=en" -o /dev/null
curl -s -b $DIR/cookieN.txt -c $DIR/cookieN.txt -o /dev/null -X POST $BASE/login \
  -d "csrf_token=$(csrf $DIR/cookieN.txt)" -d "identifier=bilal_t" -d "password=supersecret1"
N=$DIR/cookieN.txt
curl -s -b $N -c $N -o /dev/null -X POST $BASE/play/start \
  -d "csrf_token=$(csrf $N)" -d "difficulty=1" -d "count=5"
curl -s -b $N -o $DIR/nround.html $BASE/play/round
NPOS=$(grep -o 'data-position="[0-9]*"' $DIR/nround.html | head -1 | sed 's/[^0-9]//g')
contains "round carries the note box" 'data-note' $DIR/nround.html

check "note before answering is refused" 409 "$(curl -s -b $N -o /dev/null -w '%{http_code}' \
  -X POST $BASE/play/comment -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $(csrf $N)" -d "{\"position\":$NPOS,\"body\":\"far too early\"}")"

curl -s -b $N -o /dev/null -X POST $BASE/play/answer -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $(csrf $N)" -d "{\"position\":$NPOS,\"choice\":0,\"timeMs\":2500}"

check "note after answering is saved" 200 "$(curl -s -b $N -o /dev/null -w '%{http_code}' \
  -X POST $BASE/play/comment -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $(csrf $N)" -d "{\"position\":$NPOS,\"body\":\"SMOKE NOTE about this question\"}")"
check "a one-character note is refused" 422 "$(curl -s -b $N -o /dev/null -w '%{http_code}' \
  -X POST $BASE/play/comment -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $(csrf $N)" -d "{\"position\":$NPOS,\"body\":\"x\"}")"
check "a second note edits the first" 200 "$(curl -s -b $N -o /dev/null -w '%{http_code}' \
  -X POST $BASE/play/comment -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $(csrf $N)" -d "{\"position\":$NPOS,\"body\":\"SMOKE NOTE edited\"}")"

ROWS=$(docker exec -i "${DB_CONTAINER:-siraj-game-db}" psql -U "${DB_USER:-siraj}" \
  -d "${DB_NAME:-siraj-db}" -tAc \
  "SELECT count(*) FROM question_comments WHERE body LIKE 'SMOKE NOTE%';" 2>/dev/null)
check "editing did not add a row" 1 "$ROWS"

QID=$(docker exec -i "${DB_CONTAINER:-siraj-game-db}" psql -U "${DB_USER:-siraj}" \
  -d "${DB_NAME:-siraj-db}" -tAc \
  "SELECT question_id FROM question_comments WHERE body LIKE 'SMOKE NOTE%' LIMIT 1;" 2>/dev/null)
check "the thread renders" 200 "$(code x $DIR/thread.html -b $N "$BASE/questions/$QID/comments")"
contains "the edited note is shown" "SMOKE NOTE edited" $DIR/thread.html

# A player who has never seen the question must not reach its thread, or the
# comment page becomes a way to read questions early. A fresh account is used
# rather than an existing jar, because the suite signs several of them out.
rm -f $DIR/cookieX.txt
curl -s -c $DIR/cookieX.txt "$BASE/register?lang=en" -o /dev/null
curl -s -b $DIR/cookieX.txt -c $DIR/cookieX.txt -o /dev/null -X POST $BASE/register \
  -d "csrf_token=$(csrf $DIR/cookieX.txt)" -d "username=nosy_stranger" \
  -d "email=nosy@example.com" -d "display_name=Nosy Stranger" \
  -d "password=supersecret1" -d "password_confirm=supersecret1"
check "a stranger cannot open the thread" 404 "$(code x /dev/null -b $DIR/cookieX.txt "$BASE/questions/$QID/comments")"
# ...but a moderator can, because moderating the note requires reading it.
check "a moderator can open the thread" 200 "$(code x /dev/null -b "$A" "$BASE/questions/$QID/comments")"

echo
echo "== the friend button shows the real state =="
# The search list used to offer "Add friend" for everyone, including people
# already accepted. Each state must now render its own action.
curl -s -b "$A" -o $DIR/search1.html "$BASE/friends?tab=search&q=bilal&lang=en"
contains "an accepted friend reads Friends" ">Friends<" $DIR/search1.html
contains "and offers to remove" 'action="/friends/remove"' $DIR/search1.html
if grep -q '>Add friend<' $DIR/search1.html; then
  echo "  FAIL still offers to befriend an existing friend"; fail=$((fail+1))
else
  echo "  ok   does not offer to befriend an existing friend"; pass=$((pass+1))
fi

# A stranger is still offered the request.
curl -s -b "$A" -o $DIR/search2.html "$BASE/friends?tab=search&q=nosy&lang=en"
contains "a stranger is offered Add friend" ">Add friend<" $DIR/search2.html

# Ask, then confirm the button turns into "Request sent".
curl -s -b "$A" -o /dev/null -X POST $BASE/friends/request \
  -d "csrf_token=$(csrf "$A")" -d "username=nosy_stranger"
curl -s -b "$A" -o $DIR/search3.html "$BASE/friends?tab=search&q=nosy&lang=en"
contains "a pending request reads Request sent" ">Request sent<" $DIR/search3.html

# The other side sees it as incoming, with accept and decline.
curl -s -b $DIR/cookieX.txt -o $DIR/search4.html "$BASE/friends?tab=search&q=aisha&lang=en"
contains "the other side sees it as incoming" ">Wants to be friends<" $DIR/search4.html
contains "and can decline" 'action="/friends/decline"' $DIR/search4.html

# Declining removes the row, so the button goes back to the start — asking
# again must be possible rather than being stuck on "sent" for good.
curl -s -b $DIR/cookieX.txt -o /dev/null -X POST $BASE/friends/decline \
  -d "csrf_token=$(csrf $DIR/cookieX.txt)" -d "username=aisha_t"
curl -s -b "$A" -o $DIR/search5.html "$BASE/friends?tab=search&q=nosy&lang=en"
contains "after a decline it resets to Add friend" ">Add friend<" $DIR/search5.html

echo
echo
echo "== the question editor =="
# There used to be no way to author or correct a question through the UI at all.
check "editor opens" 200 "$(code x /dev/null -b "$A" "$BASE/admin/questions/new?lang=en")"
check "create a question" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST "$BASE/admin/questions/save?lang=en" \
  -d "csrf_token=$(csrf "$A")" -d "category_id=1" -d "difficulty=1" -d "points=0" \
  -d "correct_index=2" -d "is_active=1" \
  -d "prompt_en=ZZ editor fixture: which colour is the sky on a clear day?" \
  -d "choice_en_0=Red" -d "choice_en_1=Green" -d "choice_en_2=Blue" -d "choice_en_3=Yellow" \
  -d "explanation_en=Fixture row, deliberately unlike any seeded question.")"
curl -s -b "$A" -o $DIR/qlist.html "$BASE/admin/questions?q=ZZ+editor+fixture&lang=en"
QEDIT=$(grep -oP '/admin/questions/\K[0-9]+(?=/edit)' $DIR/qlist.html | head -1)
check "the new question has an id" "yes" "$([ -n "$QEDIT" ] && echo yes || echo no)"
check "edit form loads it" 200 "$(code x $DIR/qedit.html -b "$A" "$BASE/admin/questions/$QEDIT/edit?lang=en")"
contains "edit form carries the choices" 'value="Blue"' $DIR/qedit.html

# A prompt that already reads the same is warned about, not silently duplicated.
check "near-duplicate is warned" 422 "$(curl -s -b "$A" -o $DIR/qdup.html -w '%{http_code}' -X POST "$BASE/admin/questions/save?lang=en" \
  -d "csrf_token=$(csrf "$A")" -d "category_id=1" -d "difficulty=1" -d "correct_index=2" -d "is_active=1" \
  -d "prompt_en=ZZ editor fixture: which colour is the sky on a clear day?" \
  -d "choice_en_0=Red" -d "choice_en_1=Green" -d "choice_en_2=Blue" -d "choice_en_3=Yellow" \
  -d "explanation_en=x")"
contains "duplicate warning names the match" "almost the same" $DIR/qdup.html

check "duplicate choices refused" 422 "$(curl -s -b "$A" -o $DIR/qbad.html -w '%{http_code}' -X POST "$BASE/admin/questions/save?lang=en" \
  -d "csrf_token=$(csrf "$A")" -d "category_id=1" -d "difficulty=1" -d "correct_index=0" -d "is_active=1" \
  -d "prompt_en=ZZ second fixture with entirely unrelated wording throughout" \
  -d "choice_en_0=Same" -d "choice_en_1=Same" -d "choice_en_2=C" -d "choice_en_3=D" -d "explanation_en=x")"
contains "it says which rule failed" "cannot be the same" $DIR/qbad.html

# Deleting a question with recorded answers takes two presses.
check "delete warns first" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST \
  "$BASE/admin/questions/$QEDIT/delete" -d "csrf_token=$(csrf "$A")")"
check "delete goes through on confirm" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST \
  "$BASE/admin/questions/$QEDIT/delete" -d "csrf_token=$(csrf "$A")" -d "confirm=delete")"
curl -s -b "$A" -o $DIR/qgone.html "$BASE/admin/questions?q=ZZ+editor+fixture&lang=en"
if grep -q "ZZ editor fixture: which colour" $DIR/qgone.html; then
  echo "  FAIL the question survived deletion"; fail=$((fail+1))
else
  echo "  ok   the question is gone"; pass=$((pass+1))
fi

echo
echo "== clearing out questions in bulk =="
# Three throwaway questions, cleared the way an admin clears a bad import:
# tick the rows, or hand the whole search to the server.
{ echo "id,category,difficulty,points,correct,locale,prompt,choice1,choice2,choice3,choice4,explanation"
  echo "9301,quran,1,10,0,en,ZZ bulk fixture about a carpenter measuring copper wire,A,B,C,D,ZZ explanation"
  echo "9302,quran,1,10,0,en,ZZ bulk fixture about a sailor folding canvas bags,A,B,C,D,ZZ explanation"
  echo "9303,quran,1,10,0,en,ZZ bulk fixture about a gardener watering clay pots,A,B,C,D,ZZ explanation"
} > $DIR/bulk.csv
# Preview, then apply by token — the same two presses the screen makes.
curl -s -b "$A" -o $DIR/bulkprev.html -X POST "$BASE/admin/questions/import" \
  -F "csrf_token=$(csrf "$A")" -F "file=@$DIR/bulk.csv;type=text/csv"
BULK_STASH=$(grep -oP 'name="stash" value="\K[0-9a-f]+' $DIR/bulkprev.html)
curl -s -b "$A" -o /dev/null -X POST "$BASE/admin/questions/import" \
  -d "csrf_token=$(csrf "$A")" -d "commit=1" -d "stash=$BULK_STASH"

code x $DIR/bulklist.html -b "$A" "$BASE/admin/questions?q=ZZ+bulk+fixture&lang=en" > /dev/null
contains "rows offer a checkbox" 'data-bulk-item' $DIR/bulklist.html
contains "the bulk bar is there" 'data-bulk-bar' $DIR/bulklist.html
# Which of activate/deactivate is worth offering is decided from the selection,
# so each row has to say whether it is active. Both buttons ship hidden and the
# script shows the one that applies; sending only one from here would mean a
# mixed selection could not be given both.
contains "rows carry their state" 'data-active=' $DIR/bulklist.html
contains "activate ships hidden" 'data-bulk-activate hidden' $DIR/bulklist.html
contains "deactivate ships hidden" 'data-bulk-deactivate hidden' $DIR/bulklist.html
# The row action forms cannot sit inside the bulk form; the checkboxes join it
# by id instead. A nested form would silently break every per-row button.
NEST=$(grep -o '<form\|</form' $DIR/bulklist.html | awk '{d += ($0=="<form") ? 1 : -1; if (d>m) m=d} END {print m+0}')
check "no nested forms" 1 "$NEST"

check "bulk deactivate" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/admin/questions/bulk \
  -d "csrf_token=$(csrf "$A")" -d "action=deactivate" -d "ids=9301" -d "ids=9302")"
check "bulk delete the ticked rows" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/admin/questions/bulk \
  -d "csrf_token=$(csrf "$A")" -d "action=delete" -d "ids=9301" -d "ids=9302")"
# No ids at all: the server resolves the search itself, so this reaches rows on
# pages the admin never opened.
check "bulk delete the whole search" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/admin/questions/bulk \
  -d "csrf_token=$(csrf "$A")" -d "action=delete" -d "scope=filter" -d "q=ZZ bulk fixture")"
code x $DIR/bulkgone.html -b "$A" "$BASE/admin/questions?q=ZZ+bulk+fixture&lang=en" > /dev/null
# Matched against a prompt, not the search term: the filter box echoes the
# query back, so grepping for it would pass whether or not the rows are gone.
if grep -q "ZZ bulk fixture 1 (en)" $DIR/bulkgone.html; then
  echo "  FAIL questions survived the bulk delete"; fail=$((fail+1))
else
  echo "  ok   all three are gone"; pass=$((pass+1))
fi
# An empty selection must not be read as "everything".
check "an empty selection does nothing" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST $BASE/admin/questions/bulk \
  -d "csrf_token=$(csrf "$A")" -d "action=delete")"
check "the bank is untouched" 200 "$(code x $DIR/bankstill.html -b "$A" "$BASE/admin/questions?lang=en")"
contains "still listing questions" 'data-bulk-item' $DIR/bankstill.html

echo
echo "== the review queue offers both decisions =="
# Nothing in this suite can produce a machine translation — no provider is
# configured — so flag one directly, then check the queue offers both decisions
# on exactly the locale that is waiting.
docker exec -i "${DB_CONTAINER:-siraj-game-db}" psql -U "${DB_USER:-siraj}" \
  -d "${DB_NAME:-siraj-db}" -q -c \
  "UPDATE question_translations SET needs_review = true, source = 'machine'
    WHERE question_id = (SELECT min(question_id) FROM question_translations WHERE locale = 'fr')
      AND locale = 'fr';" >/dev/null 2>&1
check "review page" 200 "$(code x $DIR/rev.html -b "$A" "$BASE/admin/review?lang=en")"
contains "approve is offered" "/approve" $DIR/rev.html
contains "reject is offered" "/reject" $DIR/rev.html
contains "missing translations are listed" "Missing translations" $DIR/rev.html
# One approve and one reject, for the single locale that is waiting — not a
# button per shipped language regardless of what was actually written. Counting
# the review actions rather than every name="locale" on the page, because the
# navbar's language switcher carries those too.
PAIRS=$(grep -oE '/admin/review/[0-9]+/(approve|reject)' $DIR/rev.html | sort -u | wc -l)
check "one approve and one reject for the waiting locale" 2 "$PAIRS"
docker exec -i "${DB_CONTAINER:-siraj-game-db}" psql -U "${DB_USER:-siraj}" \
  -d "${DB_NAME:-siraj-db}" -q -c \
  "UPDATE question_translations SET needs_review = false, source = 'seed' WHERE needs_review;" >/dev/null 2>&1

echo
echo "== comment moderation =="
# Migration 0006 built the hidden flag for this; nothing could set it.
check "moderation queue" 200 "$(code x $DIR/mod.html -b "$A" "$BASE/admin/comments?lang=en")"
CID=$(grep -oP '/admin/comments/\K[0-9]+(?=/hide)' $DIR/mod.html | head -1)
if [ -n "$CID" ]; then
  check "hide a comment" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST \
    "$BASE/admin/comments/$CID/hide" -d "csrf_token=$(csrf "$A")")"
  curl -s -b "$A" -o $DIR/mod2.html "$BASE/admin/comments?lang=en"
  contains "it reads as hidden" "Hidden" $DIR/mod2.html
  check "restore it" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST \
    "$BASE/admin/comments/$CID/show" -d "csrf_token=$(csrf "$A")")"
else
  echo "  --   no comments to moderate yet; skipping"
fi

echo
echo "== notifications are read back =="
# Seven places write these; nothing read them before.
check "notifications page" 200 "$(code x $DIR/notif.html -b "$A" "$BASE/notifications?lang=en")"
contains "a friend accept was recorded" "friend request was accepted" $DIR/notif.html
check "mark all read" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST \
  "$BASE/notifications/read" -d "csrf_token=$(csrf "$A")")"

echo
echo "== the daily round =="
# The mode existed in the schema and the history filter with nothing to start one.
check "daily starts" 303 "$(curl -s -b "$A" -o /dev/null -w '%{http_code}' -X POST \
  "$BASE/play/daily" -d "csrf_token=$(csrf "$A")")"
DAILY_AGAIN=$(curl -s -b "$A" -o /dev/null -w '%{redirect_url}' -X POST \
  "$BASE/play/daily" -d "csrf_token=$(csrf "$A")")
check "a second attempt is refused" "$BASE/history?mode=daily" "$DAILY_AGAIN"
curl -s -b "$A" -o /dev/null -X POST $BASE/play/quit -d "csrf_token=$(csrf "$A")"

echo
echo "== the setup screen only offers playable choices =="
curl -s -b "$A" -o $DIR/setup.html "$BASE/play?category=1&lang=en"
contains "counts are rendered per choice" "chip-radio__count" $DIR/setup.html
contains "the count table reaches the client" "data-availability" $DIR/setup.html
# Derived from the page's own availability table rather than assuming a thin
# category: this used to hardcode "category 1 cannot fill any difficulty",
# which stopped being true the moment the bank grew and then failed on every
# run for reasons that had nothing to do with the screen.
AVAIL=$(grep -oP 'data-availability="\K[^"]+' $DIR/setup.html | sed 's/&#34;/"/g')
WANT=$(python3 -c "
import json,sys
a = json.loads(sys.argv[1])
print(sum(1 for e in a['entries'] if e['category'] == 1 and e['difficulty'] in (1,2,3) and e['count'] < a['min']))
" "$AVAIL")
DISABLED=$(grep -o 'name=\"difficulty\" value=\"[123]\" disabled' $DIR/setup.html | wc -l)
check "thin difficulties are disabled" "$WANT" "$DISABLED"
# A category that genuinely cannot fill a round, found rather than assumed. If
# every category can fill one there is nothing to refuse, and the check says so
# instead of failing.
THIN=$(python3 -c "
import json,sys
a = json.loads(sys.argv[1])
for e in a['entries']:
    if e['category'] and e['difficulty'] and e['count'] < a['min']:
        print(e['category'], e['difficulty']); break
" "$AVAIL")
if [ -n "$THIN" ]; then
  set -- $THIN
  check "an impossible choice is refused with numbers" 422 "$(curl -s -b "$A" -o $DIR/thin.html -w '%{http_code}' \
    -X POST "$BASE/play/start?lang=en" -d "csrf_token=$(csrf "$A")" -d "category=$1" -d "difficulty=$2" -d "count=10")"
  contains "the refusal says how many there are" "a round needs 5" $DIR/thin.html
else
  echo "  ok   no category is too thin to refuse (bank is full)"; pass=$((pass+1))
fi

echo
echo "== security headers and the signed flash =="
check "CSP is sent" "yes" "$(curl -s -D - -o /dev/null $BASE/login | grep -qi '^content-security-policy:' && echo yes || echo no)"
# A flash the client wrote itself is rendered as the application's own words, so
# an unsigned one must be dropped.
curl -s -b "$A" -H 'Cookie: siraj_flash=error|Your account is closed, pay here' \
  -o $DIR/forged.html "$BASE/app?lang=en"
if grep -q "pay here" $DIR/forged.html; then
  echo "  FAIL a forged flash was rendered"; fail=$((fail+1))
else
  echo "  ok   a forged flash is ignored"; pass=$((pass+1))
fi

echo
echo "================================"
echo " passed: $pass   failed: $fail"
echo "================================"
[ "$fail" -eq 0 ]
