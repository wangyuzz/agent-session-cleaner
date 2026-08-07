package jsonl_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/jsonl"
)

func collect(t *testing.T, r io.Reader) ([]string, *jsonl.Scanner) {
	t.Helper()
	scanner := jsonl.NewScanner(r)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, string(scanner.Bytes()))
	}
	return lines, scanner
}

func TestScanner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"plain lines", "one\ntwo\n", []string{"one", "two"}},
		{"no trailing newline", "one\ntwo", []string{"one", "two"}},
		{"blank lines are dropped", "one\n\n\ntwo\n", []string{"one", "two"}},
		{"carriage returns are trimmed", "one\r\ntwo\r\n", []string{"one", "two"}},
		{"nothing at all", "", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lines, scanner := collect(t, strings.NewReader(test.input))
			if err := scanner.Err(); err != nil {
				t.Fatalf("Err() = %v", err)
			}
			if strings.Join(lines, "|") != strings.Join(test.want, "|") {
				t.Errorf("read %q, want %q", lines, test.want)
			}
		})
	}
}

// A transcript line is routinely megabytes of base64 image or tool output.
// The standard scanner gives up at 64KB, which would silently drop every
// session recorded after the first screenshot anyone pasted.
func TestScannerReadsLinesFarPastTheStandardLimit(t *testing.T) {
	t.Parallel()

	huge := strings.Repeat("x", 1<<20)
	lines, scanner := collect(t, strings.NewReader("first\n"+huge+"\nlast\n"))

	if err := scanner.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("read %d lines, want 3", len(lines))
	}
	if len(lines[1]) != len(huge) {
		t.Errorf("the long line came back at %d bytes, want %d", len(lines[1]), len(huge))
	}
	if lines[2] != "last" {
		t.Errorf("lost the line after the long one: %q", lines[2])
	}
}

// Past a certain size a line is a blob, not a conversation. Skipping it beats
// abandoning the rest of the file.
func TestScannerSkipsBlobsAndKeepsGoing(t *testing.T) {
	t.Parallel()

	blob := strings.Repeat("x", jsonl.MaxLine+1)
	lines, scanner := collect(t, strings.NewReader("first\n"+blob+"\nlast\n"))

	if err := scanner.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if strings.Join(lines, "|") != "first|last" {
		t.Errorf("read %q, want first|last", lines)
	}
}

func TestScannerReportsReadFailures(t *testing.T) {
	t.Parallel()

	broken := errors.New("disk went away")
	lines, scanner := collect(t, io.MultiReader(
		strings.NewReader("first\n"),
		&failingReader{err: broken},
	))

	if len(lines) != 1 || lines[0] != "first" {
		t.Errorf("read %q, want the line before the failure", lines)
	}
	if !errors.Is(scanner.Err(), broken) {
		t.Errorf("Err() = %v, want the read failure", scanner.Err())
	}
}

func TestContextScannerStopsBeforeReadingAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scanner := jsonl.NewContextScanner(ctx, strings.NewReader("should not be read\n"))
	if scanner.Scan() {
		t.Error("Scan succeeded after cancellation")
	}
	if !errors.Is(scanner.Err(), context.Canceled) {
		t.Errorf("Err() = %v, want context.Canceled", scanner.Err())
	}
}

type failingReader struct{ err error }

func (r *failingReader) Read([]byte) (int, error) { return 0, r.err }
