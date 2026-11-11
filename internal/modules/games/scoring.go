// Package games is the engine behind every mode: solo, live room, duel, challenge and
// this-device rounds. The rules live here and only here (NF-05); the handlers and the
// other modules call this service, never the other way round.
package games

// Scoring (GP-04), the same everywhere: a base by difficulty plus up to 10 points for
// speed. Everything is an integer; a wrong answer, a time-out and a skip score nothing.
//
//	easy 10 · medium 20 · hard 30   +   round((1 - answerTime / timeLimit) × 10)
//
// With no timer there is no speed bonus, so the base is the whole score.

// BasePoints is the base score of a question by difficulty (1 easy, 2 medium, 3 hard).
func BasePoints(difficulty int32) int32 {
	switch difficulty {
	case 1:
		return 10
	case 3:
		return 30
	default:
		return 20
	}
}

// MaxSpeedBonus is the most a fast answer can add.
const MaxSpeedBonus = 10

// Points is what a correct answer is worth. timeMS is how long the player took,
// secondsPerQuestion is the game's timer (0 = no timer).
func Points(difficulty int32, timeMS int32, secondsPerQuestion int32) int32 {
	base := BasePoints(difficulty)
	if secondsPerQuestion <= 0 {
		return base
	}
	limitMS := secondsPerQuestion * 1000
	if timeMS < 0 {
		timeMS = 0
	}
	if timeMS >= limitMS {
		return base
	}
	// round((1 - t/limit) × 10) with integers only: (limit-t)*10*2 + limit) / (limit*2)
	left := int64(limitMS - timeMS)
	bonus := (left*MaxSpeedBonus*2 + int64(limitMS)) / (int64(limitMS) * 2)
	if bonus > MaxSpeedBonus {
		bonus = MaxSpeedBonus
	}
	return base + int32(bonus)
}

// MaxPoints is what a perfect, instant game is worth; the setup screen shows it (GP-02).
func MaxPoints(difficulty int32, questionCount int32, secondsPerQuestion int32) int32 {
	per := BasePoints(difficulty)
	if secondsPerQuestion > 0 {
		per += MaxSpeedBonus
	}
	return per * questionCount
}

// MixedBase is the base used when a game mixes difficulties: the medium one, so the
// summary on the setup screen stays honest.
const MixedBase = 20

// XPForGame turns a finished game into experience points: one XP for every two points
// scored, plus a bonus for a perfect round.
func XPForGame(score int32, correct int32, total int32) int32 {
	xp := score / 2
	if total > 0 && correct == total {
		xp += 10
	}
	return xp
}

// LevelFor is the level a player with this much XP has reached. Level n needs
// 100 × n × (n+1) / 2 XP in total, so the levels get wider as they go.
func LevelFor(xp int32) int32 {
	level := int32(1)
	for XPForLevel(level+1) <= xp {
		level++
		if level > 999 {
			break
		}
	}
	return level
}

// XPForLevel is the total XP needed to reach a level.
func XPForLevel(level int32) int32 {
	if level <= 1 {
		return 0
	}
	n := level - 1
	return 100 * n * (n + 1) / 2
}
