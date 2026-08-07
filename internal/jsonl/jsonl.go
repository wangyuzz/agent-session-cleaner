// Package jsonl reads the newline-delimited JSON used by transcript-backed agents.
//
// It exists because bufio.Scanner cannot: a single transcript line is
// routinely megabytes of base64 image or tool output, and the standard
// scanner abandons the rest of the file the moment it meets one.
package jsonl

import (
	"bufio"
	"context"
	"errors"
	"io"
)

// MaxLine is the largest line worth reading. Anything past it is a blob rather
// than a conversation, and no field this program looks for lives in one.
const MaxLine = 16 << 20

// Scanner walks a transcript one line at a time. Lines too large to be
// conversation are skipped, and reading continues — losing one attachment is
// better than losing every session recorded after it.
type Scanner struct {
	reader   *bufio.Reader
	buf      []byte
	line     []byte
	err      error
	finished bool
}

// NewScanner reads JSONL from r.
func NewScanner(r io.Reader) *Scanner {
	return &Scanner{reader: bufio.NewReaderSize(r, 64<<10)}
}

// NewContextScanner is NewScanner with cancellation checked before every
// underlying read. Regular-file reads are usually immediate, but a long
// transcript should still stop being parsed when its Bubble Tea command is
// cancelled.
func NewContextScanner(ctx context.Context, r io.Reader) *Scanner {
	return NewScanner(contextReader{ctx: ctx, reader: r})
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// Scan advances to the next line, reporting false at the end of the input or
// on a read error.
func (s *Scanner) Scan() bool {
	for !s.finished {
		line, oversized, err := s.read()
		switch {
		case oversized:
			continue
		case len(line) > 0:
			s.line = line
			if err != nil {
				s.stop(err)
			}
			return true
		case err != nil:
			s.stop(err)
			return false
		}
	}
	return false
}

// read accumulates one line. A line is returned without its terminator; the
// second result reports one too large to be worth keeping.
func (s *Scanner) read() (line []byte, oversized bool, err error) {
	s.buf = s.buf[:0]
	for {
		chunk, readErr := s.reader.ReadSlice('\n')
		if !oversized {
			if len(s.buf)+len(chunk) > MaxLine {
				oversized, s.buf = true, s.buf[:0]
			} else {
				s.buf = append(s.buf, chunk...)
			}
		}
		if errors.Is(readErr, bufio.ErrBufferFull) {
			continue // more of this line is still coming
		}
		return trimNewline(s.buf), oversized, readErr
	}
}

func (s *Scanner) stop(err error) {
	s.finished = true
	if !errors.Is(err, io.EOF) {
		s.err = err
	}
}

func trimNewline(line []byte) []byte {
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
		if n = len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
	}
	return line
}

// Bytes is the current line, valid until the next call to Scan.
func (s *Scanner) Bytes() []byte { return s.line }

// Err is the read error that ended the scan, if it was not simply the end of
// the input.
func (s *Scanner) Err() error { return s.err }
