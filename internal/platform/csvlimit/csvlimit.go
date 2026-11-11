// Package csvlimit holds the one pair of numbers every CSV import in the project
// works to: the admin console's domains, categories, questions and players (A04,
// A05, A06, A02) and a player's own question set all read the same size of file.
//
// They live here, outside any module, because the screens say the numbers out loud
// and a page may not import a module. One constant each means the staff hint, the
// "too big" message and the reader that enforces them can never drift apart.
package csvlimit

const (
	// Rows is the most lines one file may carry, the header not counted.
	Rows = 5000

	// Bytes is the biggest file the importer reads. 5 000 question rows carry a
	// question, four answers and an explanation in three languages, and Arabic
	// spends two bytes a letter, so the room is given rather than measured.
	Bytes = 24 << 20

	// MB is Bytes as the screens print it.
	MB = Bytes >> 20
)
