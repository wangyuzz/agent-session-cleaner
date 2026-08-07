// Package i18n renders user-facing text in English or Chinese.
//
// A [Printer] is a value, constructed once at startup and passed to whatever
// needs to speak. Nothing here reads the environment on its own, so tests can
// run both languages side by side and in parallel.
package i18n

import (
	"fmt"
	"strings"
)

// The languages this program speaks.
const (
	English = "en"
	Chinese = "zh"
)

// Args names the values a message interpolates. A template refers to them as
// {name}; a name with no value is left in the output rather than silently
// dropped, so the gap is visible.
type Args map[string]any

// Printer renders keys in one language.
type Printer struct {
	catalog map[Key]string
}

// New returns a printer for the given language, falling back to English for
// anything it does not speak.
func New(lang string) *Printer {
	if lang == Chinese {
		return &Printer{catalog: chinese}
	}
	return &Printer{catalog: english}
}

// T renders one message. At most one Args may be given.
func (p *Printer) T(key Key, args ...Args) string {
	template, ok := p.catalog[key]
	if !ok {
		// Every key is covered in both catalogues, and a test says so. Falling
		// back to English keeps a slip readable rather than blank.
		if template, ok = english[key]; !ok {
			return fmt.Sprintf("!i18n(%d)", int(key))
		}
	}
	if len(args) == 0 {
		return template
	}
	return expand(template, args[0])
}

// N renders the singular or plural wording for count, which is also made
// available to the template as {count}.
func (p *Printer) N(one, many Key, count int, args ...Args) string {
	values := Args{"count": count}
	if len(args) > 0 {
		for name, value := range args[0] {
			values[name] = value
		}
	}
	key := many
	if count == 1 {
		key = one
	}
	return p.T(key, values)
}

// singulars pairs a category name with its singular form. Chinese has no
// plural forms, so both entries there are the same words.
var singulars = map[Key]Key{
	SessionsWord:     SessionsWordOne,
	ArchivedSessions: ArchivedSessionsOne,
	EmptySessions:    EmptySessionsOne,
	OrphanSessions:   OrphanSessionsOne,
}

// Label renders a category name — "archived sessions", "empty sessions" — in
// the form that agrees with count.
func (p *Printer) Label(key Key, count int) string {
	if count == 1 {
		if one, ok := singulars[key]; ok {
			return p.T(one)
		}
	}
	return p.T(key)
}

// expand substitutes {name} placeholders.
func expand(template string, args Args) string {
	if !strings.ContainsRune(template, '{') {
		return template
	}
	var out strings.Builder
	out.Grow(len(template))
	for {
		open := strings.IndexByte(template, '{')
		if open < 0 {
			break
		}
		closed := strings.IndexByte(template[open:], '}')
		if closed < 0 {
			break
		}
		closed += open
		name := template[open+1 : closed]
		value, ok := args[name]
		out.WriteString(template[:open])
		if ok {
			fmt.Fprint(&out, value)
		} else {
			out.WriteString(template[open : closed+1])
		}
		template = template[closed+1:]
	}
	out.WriteString(template)
	return out.String()
}

// LanguageEnv is the variable that overrides locale detection.
const LanguageEnv = "ASC_LANG"

// Detect picks a language from the environment. Pass os.Getenv; a stub keeps
// tests independent of the machine they run on.
//
// An explicit override wins. Otherwise the first locale variable that is set
// decides, even if it names a language this program does not speak — a French
// locale means "not Chinese", and English is the better answer than guessing.
func Detect(lookup func(string) string) string {
	if lang := normalize(lookup(LanguageEnv)); lang != "" {
		return lang
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANGUAGE", "LANG"} {
		if value := lookup(name); value != "" {
			if lang := normalize(value); lang != "" {
				return lang
			}
			return English
		}
	}
	return English
}

func normalize(value string) string {
	if value == "" {
		return ""
	}
	// LANGUAGE holds a colon-separated preference list; the first entry wins.
	first, _, _ := strings.Cut(value, ":")
	first = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(first), "-", "_"))
	switch {
	case strings.HasPrefix(first, "zh"):
		return Chinese
	case strings.HasPrefix(first, "en"), first == "c", first == "posix":
		return English
	}
	return ""
}
