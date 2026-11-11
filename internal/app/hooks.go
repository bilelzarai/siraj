package app

import (
	"context"

	"siraj/internal/db/sqlc"
	"siraj/internal/modules/games"
	"siraj/internal/modules/progress"
)

// progressHook joins the games engine to the progress module without either package
// importing the other: games publishes the result, progress writes the consequences,
// both on the finish transaction's queries.
type progressHook struct{ p *progress.Service }

func (h progressHook) OnGameFinished(ctx context.Context, q *sqlc.Queries, r games.FinishResult) error {
	return h.p.Apply(ctx, q, progress.Finished{
		GameID: r.Game.ID, PlayerID: r.PlayerID, UserID: r.UserID,
		Mode:  string(r.Game.Mode),
		Score: r.Score, Correct: r.Correct, Total: r.Total, XP: r.XP,
	})
}
