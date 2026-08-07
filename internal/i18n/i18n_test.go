package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Both catalogues are reached directly, so this file stays in the package.

func TestCatalogsCoverEveryKey(t *testing.T) {
	t.Parallel()

	for key := invalid + 1; key < numKeys; key++ {
		if _, ok := english[key]; !ok {
			t.Errorf("key %d has no English text", int(key))
		}
		if _, ok := chinese[key]; !ok {
			t.Errorf("key %d has no Chinese text", int(key))
		}
	}
	if len(english) != int(numKeys)-1 {
		t.Errorf("English catalogue holds %d entries, want %d", len(english), int(numKeys)-1)
	}
	if len(chinese) != int(numKeys)-1 {
		t.Errorf("Chinese catalogue holds %d entries, want %d", len(chinese), int(numKeys)-1)
	}
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

func names(template string) string {
	found := placeholder.FindAllStringSubmatch(template, -1)
	seen := make([]string, 0, len(found))
	for _, match := range found {
		seen = append(seen, match[1])
	}
	sort.Strings(seen)
	return strings.Join(seen, ",")
}

// A translation that drops a placeholder loses a session id or a file path
// from a message that exists to name one.
func TestTranslationsKeepTheSamePlaceholders(t *testing.T) {
	t.Parallel()

	for key := invalid + 1; key < numKeys; key++ {
		if want, got := names(english[key]), names(chinese[key]); want != got {
			t.Errorf("key %d: English uses {%s}, Chinese uses {%s}", int(key), want, got)
		}
	}
}

func TestT(t *testing.T) {
	t.Parallel()

	p := New(English)

	if got := p.T(Today); got != "Today" {
		t.Errorf("T(Today) = %q", got)
	}
	got := p.T(CopySessionIDSuccess, Args{"session_id": "abc"})
	if want := "Session ID copied to clipboard: abc"; got != want {
		t.Errorf("T(CopySessionIDSuccess) = %q, want %q", got, want)
	}
	// A name with no value stays visible instead of leaving a hole.
	if got := p.T(SpawnedBy); !strings.Contains(got, "{session_id}") {
		t.Errorf("T with no args = %q, want the placeholder left in place", got)
	}
}

func TestN(t *testing.T) {
	t.Parallel()

	p := New(English)
	if got := p.N(MessagesOne, MessagesMany, 1); got != "1 message" {
		t.Errorf("N(1) = %q", got)
	}
	if got := p.N(MessagesOne, MessagesMany, 4); got != "4 messages" {
		t.Errorf("N(4) = %q", got)
	}
	// Extra values travel alongside the count.
	got := p.N(BannerOrphansOne, BannerOrphansMany, 2, Args{"what": "orphans"})
	if got != "2 orphans" {
		t.Errorf("N with extra args = %q", got)
	}
}

func TestLabelAgreesWithCount(t *testing.T) {
	t.Parallel()

	en := New(English)
	if got := en.Label(ArchivedSessions, 1); got != "archived session" {
		t.Errorf("Label(1) = %q, want the singular", got)
	}
	if got := en.Label(ArchivedSessions, 3); got != "archived sessions" {
		t.Errorf("Label(3) = %q, want the plural", got)
	}

	// Chinese has no plural form; the same words serve either count.
	zh := New(Chinese)
	if zh.Label(ArchivedSessions, 1) != zh.Label(ArchivedSessions, 3) {
		t.Error("Chinese labels differ by count")
	}
}

func TestNewFallsBackToEnglish(t *testing.T) {
	t.Parallel()

	if got := New("fr").T(Today); got != "Today" {
		t.Errorf("French fallback said %q", got)
	}
	if got := New(Chinese).T(Today); got != "今天" {
		t.Errorf("Chinese printer said %q", got)
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"nothing set", nil, English},
		{"explicit override", map[string]string{LanguageEnv: "zh"}, Chinese},
		{"override beats the locale", map[string]string{
			LanguageEnv: "en", "LANG": "zh_CN.UTF-8",
		}, English},
		{"an unusable override falls through", map[string]string{
			LanguageEnv: "klingon", "LANG": "zh_CN.UTF-8",
		}, Chinese},
		{"LC_ALL wins over LANG", map[string]string{
			"LC_ALL": "en_US.UTF-8", "LANG": "zh_CN.UTF-8",
		}, English},
		{"LANGUAGE takes its first entry", map[string]string{
			"LANGUAGE": "zh_CN:en_US",
		}, Chinese},
		{"a hyphenated tag", map[string]string{"LANG": "zh-Hans"}, Chinese},
		{"the POSIX locale is English", map[string]string{"LANG": "C"}, English},
		// A locale this program does not speak still answers the question it
		// was asked: it is not Chinese.
		{"an unspoken language", map[string]string{"LANG": "fr_FR.UTF-8"}, English},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lookup := func(name string) string { return test.env[name] }
			if got := Detect(lookup); got != test.want {
				t.Errorf("Detect() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		args     Args
		want     string
	}{
		{"no placeholders", "plain", Args{"a": 1}, "plain"},
		{"one value", "{a}!", Args{"a": "x"}, "x!"},
		{"repeated name", "{a}-{a}", Args{"a": 2}, "2-2"},
		{"unclosed brace", "{a", Args{"a": 1}, "{a"},
		{"unknown name", "{b}", Args{"a": 1}, "{b}"},
		{"surrounding text", "n={n} of {total}", Args{"n": 1, "total": 9}, "n=1 of 9"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := expand(test.template, test.args); got != test.want {
				t.Errorf("expand(%q) = %q, want %q", test.template, got, test.want)
			}
		})
	}
}
