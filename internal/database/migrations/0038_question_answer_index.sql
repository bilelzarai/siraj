-- =============================================================================
-- 0038_question_answer_index.sql — reading answers by question, not by session
--
-- 0001 indexed game_answers by (session_id, position), which is the shape the
-- player's own round is read in. Every admin judgement about a question asks
-- the opposite question — how often has this one been answered, and how often
-- correctly — and had no index to ask it with: a sequential scan of every
-- answer ever recorded, per row of a fifty-row table.
--
-- The question browser, the category performance table and the accuracy figure
-- on the dashboard all aggregate through this index.
-- =============================================================================

CREATE INDEX IF NOT EXISTS game_answers_question_idx
    ON game_answers (question_id) INCLUDE (is_correct);

-- The dashboard's time series groups finished rounds by day. Only finished
-- rounds count, so the condition belongs in the index rather than in each
-- query that reads it.
CREATE INDEX IF NOT EXISTS game_sessions_finished_idx
    ON game_sessions (finished_at) WHERE finished_at IS NOT NULL;
