// Package buildinfo carries the one place the version number is written.
package buildinfo

// Name is the program as it introduces itself on the command line.
const Name = "agent-session-cleaner"

// Version is read by --version and checked against the release tag, so the
// two can never drift apart.
const Version = "0.8.1"
