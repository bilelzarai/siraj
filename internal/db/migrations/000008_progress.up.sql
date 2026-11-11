-- Module 8 · Progress: XP, mastery, streaks, activity, achievements, leaderboards.
-- Everything here is written inside the game-finish transaction (games.FinishHook).

-- Mastery per category, 0–100. Domain mastery is the average over its categories.
CREATE TABLE user_category_stats (
  user_id        uuid   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  category_id    bigint NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
  answered       integer NOT NULL DEFAULT 0,
  correct        integer NOT NULL DEFAULT 0,
  mastery        smallint NOT NULL DEFAULT 0 CHECK (mastery BETWEEN 0 AND 100),
  last_played_at timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, category_id)
);
CREATE TRIGGER user_category_stats_updated_at BEFORE UPDATE ON user_category_stats
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX user_category_stats_recent ON user_category_stats (user_id, last_played_at DESC);

CREATE TABLE user_domain_stats (
  user_id        uuid   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  domain_id      bigint NOT NULL REFERENCES domains (id) ON DELETE CASCADE,
  games_played   integer NOT NULL DEFAULT 0,
  answered       integer NOT NULL DEFAULT 0,
  correct        integer NOT NULL DEFAULT 0,
  mastery        smallint NOT NULL DEFAULT 0 CHECK (mastery BETWEEN 0 AND 100),
  last_played_at timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, domain_id)
);
CREATE TRIGGER user_domain_stats_updated_at BEFORE UPDATE ON user_domain_stats
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- One row per player per day: streaks, the daily goal and the 12-week grid.
CREATE TABLE daily_activity (
  user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  day     date NOT NULL,
  games   integer NOT NULL DEFAULT 0,
  points  integer NOT NULL DEFAULT 0,
  correct integer NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, day)
);
CREATE INDEX daily_activity_day ON daily_activity (day);

-- The XP ledger; users.xp is its cached sum.
CREATE TABLE xp_events (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  amount     integer NOT NULL,
  reason     text NOT NULL,      -- game | daily_question | invite | achievement | challenge
  game_id    uuid REFERENCES games (id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX xp_events_user ON xp_events (user_id, id DESC);

CREATE TABLE achievements (
  id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code     text NOT NULL UNIQUE,
  icon     text NOT NULL DEFAULT 'star',
  color    text NOT NULL DEFAULT '#c08a24',
  target   integer NOT NULL DEFAULT 1,
  kind     text NOT NULL DEFAULT 'counter',  -- counter | streak | mastery
  position smallint NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE achievement_translations (
  achievement_id bigint NOT NULL REFERENCES achievements (id) ON DELETE CASCADE,
  lang           text   NOT NULL REFERENCES languages (code),
  name           text   NOT NULL,
  description    text   NOT NULL DEFAULT '',
  PRIMARY KEY (achievement_id, lang)
);

CREATE TABLE user_achievements (
  user_id        uuid   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  achievement_id bigint NOT NULL REFERENCES achievements (id) ON DELETE CASCADE,
  progress       integer NOT NULL DEFAULT 0,
  unlocked_at    timestamptz,
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, achievement_id)
);
CREATE TRIGGER user_achievements_updated_at BEFORE UPDATE ON user_achievements
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Weekly points, global (domain_id IS NULL) and per domain. Upserted on game finish.
CREATE TABLE leaderboard_weekly (
  week_start date   NOT NULL,
  domain_id  bigint REFERENCES domains (id) ON DELETE CASCADE,
  user_id    uuid   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  points     integer NOT NULL DEFAULT 0,
  games      integer NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX leaderboard_weekly_global ON leaderboard_weekly (week_start, user_id) WHERE domain_id IS NULL;
CREATE UNIQUE INDEX leaderboard_weekly_domain ON leaderboard_weekly (week_start, domain_id, user_id) WHERE domain_id IS NOT NULL;
CREATE INDEX leaderboard_weekly_top ON leaderboard_weekly (week_start, domain_id, points DESC);

-- Month and all time are rollups the worker refreshes (T-51); they read the same table.
CREATE MATERIALIZED VIEW leaderboard_monthly AS
  SELECT date_trunc('month', week_start)::date AS month_start,
         domain_id, user_id, sum(points)::integer AS points, sum(games)::integer AS games
  FROM leaderboard_weekly GROUP BY 1, 2, 3;
CREATE UNIQUE INDEX leaderboard_monthly_global ON leaderboard_monthly (month_start, user_id) WHERE domain_id IS NULL;
CREATE UNIQUE INDEX leaderboard_monthly_domain ON leaderboard_monthly (month_start, domain_id, user_id) WHERE domain_id IS NOT NULL;

CREATE MATERIALIZED VIEW leaderboard_alltime AS
  SELECT domain_id, user_id, sum(points)::integer AS points, sum(games)::integer AS games
  FROM leaderboard_weekly GROUP BY 1, 2;
CREATE UNIQUE INDEX leaderboard_alltime_global ON leaderboard_alltime (user_id) WHERE domain_id IS NULL;
CREATE UNIQUE INDEX leaderboard_alltime_domain ON leaderboard_alltime (domain_id, user_id) WHERE domain_id IS NOT NULL;

-- One view over the three periods, so every leaderboard read is the same query.
CREATE VIEW leaderboard_periods AS
  SELECT 'week'::text  AS period, week_start   AS period_start, domain_id, user_id, points, games FROM leaderboard_weekly
  UNION ALL
  SELECT 'month'::text AS period, month_start  AS period_start, domain_id, user_id, points, games FROM leaderboard_monthly
  UNION ALL
  SELECT 'all'::text   AS period, DATE '1970-01-01' AS period_start, domain_id, user_id, points, games FROM leaderboard_alltime;

-- Seed achievements (three languages, as every player-visible text must be).
INSERT INTO achievements (code, icon, color, target, kind, position) VALUES
  ('first_game',   'play',   '#1f7a4d', 1,   'counter', 1),
  ('games_50',     'trophy', '#c08a24', 50,  'counter', 2),
  ('streak_7',     'flame',  '#c2410c', 7,   'streak',  3),
  ('streak_30',    'flame',  '#b4372c', 30,  'streak',  4),
  ('perfect_game', 'star',   '#862e9c', 1,   'counter', 5),
  ('mastery_80',   'medal',  '#0b7a5c', 80,  'mastery', 6);

INSERT INTO achievement_translations (achievement_id, lang, name, description)
SELECT a.id, t.lang, t.name, t.description
FROM achievements a
JOIN (VALUES
  ('first_game',  'en', 'First steps',      'Finish your first game'),
  ('first_game',  'fr', 'Premiers pas',     'Terminez votre première partie'),
  ('first_game',  'ar', 'الخطوة الأولى',    'أنهِ أول لعبة لك'),
  ('games_50',    'en', 'Regular',          'Finish 50 games'),
  ('games_50',    'fr', 'Régulier',         'Terminez 50 parties'),
  ('games_50',    'ar', 'مواظب',            'أنهِ 50 لعبة'),
  ('streak_7',    'en', 'One week',         'Play 7 days in a row'),
  ('streak_7',    'fr', 'Une semaine',      'Jouez 7 jours de suite'),
  ('streak_7',    'ar', 'أسبوع كامل',       'العب 7 أيام متتالية'),
  ('streak_30',   'en', 'One month',        'Play 30 days in a row'),
  ('streak_30',   'fr', 'Un mois',          'Jouez 30 jours de suite'),
  ('streak_30',   'ar', 'شهر كامل',         'العب 30 يومًا متتاليًا'),
  ('perfect_game','en', 'Flawless',         'Answer every question of a game correctly'),
  ('perfect_game','fr', 'Sans faute',       'Répondez correctement à toutes les questions'),
  ('perfect_game','ar', 'بلا خطأ',          'أجب عن كل أسئلة اللعبة بشكل صحيح'),
  ('mastery_80',  'en', 'Expert',           'Reach 80% mastery in a domain'),
  ('mastery_80',  'fr', 'Expert',           'Atteignez 80 % de maîtrise dans un domaine'),
  ('mastery_80',  'ar', 'خبير',             'بلوغ 80٪ من الإتقان في مجال')
) AS t (code, lang, name, description) ON t.code = a.code;
