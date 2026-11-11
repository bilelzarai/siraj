package admin

import (
	"fmt"
	"strings"
	"testing"

	"siraj/internal/platform/csvlimit"
)

// file writes a domains CSV with n rows under the header the importer expects.
func file(n int) string {
	var b strings.Builder
	b.WriteString(strings.Join(DomainColumns, ",") + "\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, ",dom-%d,islamic,mosque,#0b7a5c,mosque,%d,no,yes,Name %d,اسم %d,Nom %d,,,\n", i, i, i, i, i)
	}
	return b.String()
}

// A file of exactly the cap is read whole: the number the screens promise is the
// number the reader takes.
func TestReadTakesTheFullRowCap(t *testing.T) {
	records, problems, err := read(strings.NewReader(file(csvlimit.Rows)), DomainColumns)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("want no problems, got %v", problems)
	}
	if len(records) != csvlimit.Rows {
		t.Fatalf("read %d rows, want %d", len(records), csvlimit.Rows)
	}
}

// One row past the cap is refused as a whole file rather than half-read: the extra
// line becomes a problem, and a report with a problem never writes anything.
func TestReadRefusesPastTheRowCap(t *testing.T) {
	records, problems, err := read(strings.NewReader(file(csvlimit.Rows+1)), DomainColumns)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != csvlimit.Rows {
		t.Fatalf("read %d rows, want %d", len(records), csvlimit.Rows)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Why, fmt.Sprint(csvlimit.Rows)) {
		t.Fatalf("want one problem naming the cap, got %v", problems)
	}
}

// The byte cap has to be able to hold a full file, or the row cap is a promise the
// reader cannot keep: a file cut short by io.LimitReader loses its tail in silence.
func TestByteCapHoldsAFullFile(t *testing.T) {
	if size := len(file(csvlimit.Rows)); size > csvlimit.Bytes {
		t.Fatalf("a %d-row domains file is %d bytes, over the %d-byte cap", csvlimit.Rows, size, csvlimit.Bytes)
	}
}
