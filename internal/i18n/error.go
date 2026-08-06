package i18n

import "errors"

// Error is a failure whose wording is chosen when it reaches the screen rather
// than where it happened. Anything below the interface layer can report a
// problem in the user's language without knowing which language that is.
type Error struct {
	Key  Key
	Args Args
	Err  error
}

// Errorf reports a failure by key.
func Errorf(key Key, args ...Args) *Error {
	return &Error{Key: key, Args: first(args)}
}

// Wrap adds user-facing wording to an underlying failure, which stays
// available to errors.Is and errors.As.
func Wrap(err error, key Key, args ...Args) *Error {
	return &Error{Key: key, Args: first(args), Err: err}
}

// Raw reports a failure whose text is already the message — an agent's own
// command line complaining in its own words.
func Raw(text string) *Error {
	return &Error{Key: Verbatim, Args: Args{"text": text}}
}

// plain renders errors for %v and for log files, which are English wherever
// the interface happens to be speaking.
var plain = New(English)

// Error renders in English, which is what %v and a log file want.
func (e *Error) Error() string { return plain.T(e.Key, e.Args) }

// Unwrap exposes the underlying failure.
func (e *Error) Unwrap() error { return e.Err }

// Err renders any error for the status line: one that carries its own wording
// in the user's language, anything else as an unexpected failure.
func (p *Printer) Err(err error) string {
	if err == nil {
		return ""
	}
	var localized *Error
	if errors.As(err, &localized) {
		return p.T(localized.Key, localized.Args)
	}
	return p.T(UnexpectedError, Args{"error": err.Error()})
}

func first(args []Args) Args {
	if len(args) == 0 {
		return nil
	}
	return args[0]
}
