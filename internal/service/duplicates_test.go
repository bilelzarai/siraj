package service

import (
	"testing"
	"time"
)

// The cache is what makes the content-health screen usable: the sweep behind it
// is a trigram self-join measured at 3.5 s in English.
func TestDuplicateSweepTTLIsLongEnoughToMatter(t *testing.T) {
	if DuplicateSweepTTL < time.Minute {
		t.Errorf("TTL is %v; below a minute the cache does not save the second visitor",
			DuplicateSweepTTL)
	}
}

// A second visitor arriving while the first sweep is still running waits for it
// rather than starting a second one.
func TestPendingSweepIsRecognised(t *testing.T) {
	entry := &sweepResult{ready: make(chan struct{})}
	if !isPending(entry) {
		t.Error("a sweep that has not finished was reported as done")
	}
	close(entry.ready)
	if isPending(entry) {
		t.Error("a finished sweep was reported as pending")
	}
}
