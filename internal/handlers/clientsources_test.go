package handlers_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clientSources is every line of client script, concatenated.
//
// Four tests assert the server and the client agree — waveform constants,
// event names, team minimums, class names. They were written when the client
// was one file and they read that file. It is now a tree. Reading one file of
// it would leave those tests passing while asserting nothing, which is worse
// than failing: the drift they exist to catch would go through silently.
func clientSources(t *testing.T) string {
	t.Helper()
	body, err := concatClientSources(filepath.Join(repoRoot(t), "web"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// concatClientSources is the half that takes no *testing.T, so the size floor
// below can itself be tested rather than taken on trust.
func concatClientSources(root string) (string, error) {
	var out strings.Builder

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".js" {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out.WriteString("\n// ---- " + path + "\n")
		out.Write(body)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("reading the client sources: %w", err)
	}

	// A refactor that moved the sources would otherwise make every caller pass
	// against an empty string.
	if out.Len() < 50_000 {
		return "", fmt.Errorf("only %d bytes of client source under %s; the checks "+
			"that read this would pass vacuously", out.Len(), root)
	}
	return out.String(), nil
}
