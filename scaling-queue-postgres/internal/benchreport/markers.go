package benchreport

import (
	"fmt"
	"os"
	"strings"
)

// A generated block is delimited so prose around it stays hand-written. Text
// inside the markers is replaced, so a number cannot be typed in by hand and
// then drift from the recorded results.
func Begin(name string) string { return "<!-- begin:generated:" + name + " -->" }
func End(name string) string   { return "<!-- end:generated:" + name + " -->" }

// Replace swaps the body of one named block. It fails when the block is absent,
// because a silently skipped block is how a stale number survives.
func Replace(doc, name, body string) (string, error) {
	begin, end := Begin(name), End(name)
	i := strings.Index(doc, begin)
	if i < 0 {
		return "", fmt.Errorf("no %s marker", begin)
	}
	j := strings.Index(doc[i:], end)
	if j < 0 {
		return "", fmt.Errorf("no %s marker after %s", end, begin)
	}
	j += i
	return doc[:i] + begin + "\n" + strings.TrimRight(body, "\n") + "\n" + doc[j:], nil
}

type Block struct {
	Name string
	Body string
}

// Apply writes the blocks into a file, or reports whether it would change it.
// The check mode is what makes a stale document fail the build.
func Apply(path string, blocks []Block, check bool) (changed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := string(data)
	for _, b := range blocks {
		doc, err = Replace(doc, b.Name, b.Body)
		if err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
	}
	if doc == string(data) {
		return false, nil
	}
	if check {
		return true, nil
	}
	return true, os.WriteFile(path, []byte(doc), 0o644)
}
