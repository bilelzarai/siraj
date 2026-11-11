-- REFRESH MATERIALIZED VIEW CONCURRENTLY needs one unique index over whole rows: the
-- two indexes each board already has are partial (one for the global board, one per
-- domain), which does not qualify, so the worker's refresh failed with 55000.
-- NULLS NOT DISTINCT makes the global rows — the ones with no domain — unique too.
CREATE UNIQUE INDEX leaderboard_monthly_key
  ON leaderboard_monthly (month_start, domain_id, user_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX leaderboard_alltime_key
  ON leaderboard_alltime (domain_id, user_id) NULLS NOT DISTINCT;
