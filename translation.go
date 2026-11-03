package rating

import (
	"embed"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/translation"
)

// The sentences of this package's screens, messages and activity descriptions.
// They are embedded and not published: an application that wants other words
// writes them in its own catalogue, under the same keys, and Config.Translator
// is asked first. One override path, the one the application already has.

//go:embed resources/lang
var langSources embed.FS

// TranslationGroup is the file every key of this package lives in, and the
// prefix of every key: "rating.screen.index_title".
const TranslationGroup = "rating"

var shipped, shippedLocales = mustShipped()

func mustShipped() (*translation.Translator, []string) {
	root, err := fs.Sub(langSources, "resources/lang")
	if err != nil {
		panic("rating: the embedded catalogue has no lang directory: " + err.Error())
	}
	loader, err := translation.NewFileLoader(root, ".")
	if err != nil {
		panic("rating: the embedded catalogue does not parse: " + err.Error())
	}
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		panic("rating: the embedded catalogue cannot be listed: " + err.Error())
	}
	locales := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			locales = append(locales, entry.Name())
		}
	}
	sort.Strings(locales)
	if len(locales) == 0 {
		panic("rating: the embedded catalogue holds no locale, so every screen would draw its keys")
	}
	return translation.New(loader, FallbackLocale, FallbackLocale), locales
}

// Locales are the languages the package ships.
func Locales() []string { return append([]string(nil), shippedLocales...) }

// Lines are the sentences this package ships for one locale, keyed the way a
// translator asks for them ("rating.validation.required"), and nil for a locale
// it does not ship. An application reads them to see what there is to
// override, or to check a catalogue of its own against them.
func Lines(locale string) translation.Lines {
	lines := shipped.GetLoader().Load(locale, TranslationGroup, "*")
	if len(lines) == 0 {
		return nil
	}
	out := make(translation.Lines, len(lines))
	for item, line := range lines {
		out[TranslationGroup+"."+item] = line
	}
	return out
}

// Labels are the sentences of this package in one locale.
type Labels struct {
	Locale     string
	translator *translation.Translator
}

// labelsOf are the sentences the configuration asks for.
func labelsOf(c Config) Labels {
	locale := strings.TrimSpace(c.Locale)
	if locale == "" {
		locale = DefaultLocale
	}
	return Labels{Locale: locale, translator: c.Translator}
}

// Labels answers the sentences of the module's locale.
func (m *Module) Labels() Labels { return labelsOf(m.cfg) }

// T reads a key of this package: "screen.index_title".
func (l Labels) T(key string) string { return l.line(key, nil) }

// line reads a key with its placeholders filled, asking the application's
// translator first and the shipped catalogue after it.
func (l Labels) line(key string, replace translation.Replace) string {
	full := TranslationGroup + "." + key
	if l.translator != nil && l.translator.Has(l.Locale, full) {
		return l.translator.Get(l.Locale, full, replace)
	}
	return shipped.Get(l.Locale, full, replace)
}

// has says whether any catalogue has a line for the key.
func (l Labels) has(key string) bool {
	full := TranslationGroup + "." + key
	if l.translator != nil && l.translator.Has(l.Locale, full) {
		return true
	}
	return shipped.Has(l.Locale, full) || shipped.Has(FallbackLocale, full)
}

// Status is the label of a rating's status.
func (l Labels) Status(s Status) string { return l.T("status." + string(s)) }

// ReportStatus is the label of a report's status.
func (l Labels) ReportStatus(s ReportStatus) string { return l.T("report_status." + string(s)) }

// Outcome is the label of what an upheld report did to its rating, and empty
// for a report that did nothing.
func (l Labels) Outcome(o Outcome) string {
	if o == OutcomeNone {
		return ""
	}
	return l.T("outcome." + string(o))
}

// Verdict is the label of a verdict of the moderation.
func (l Labels) Verdict(v Verdict) string { return l.T("verdict." + string(v)) }

// StarName is what the star at position n is called, from one up.
func (l Labels) StarName(n int) string { return l.T("stars." + strconv.Itoa(n)) }

// ScoreLabel is a number of stars as a filter offers it: "5 estrelas".
func (l Labels) ScoreLabel(n int) string {
	return l.line("score.label", translation.Replace{"n": n})
}

// Count writes a number of ratings with its noun: "1 avaliação", "12 avaliações".
func (l Labels) Count(n int) string {
	key := "count.other"
	if n == 1 {
		key = "count.one"
	}
	return l.line(key, translation.Replace{"n": n})
}

// ScoreValue is a score as the value of a rating group, and empty when it is
// not one.
func (l Labels) ScoreValue(n int) string { return stars(n) }

// StarNames are the names of every star, from the first up.
func (l Labels) StarNames() []string {
	out := make([]string, MaxScore)
	for i := range out {
		out[i] = l.StarName(i + 1)
	}
	return out
}

// Average writes an average the way the locale writes a decimal number: "4,3"
// in Portuguese, "4.3" in English.
func (l Labels) Average(s Summary) string {
	text := strconv.FormatFloat(s.Rounded(), 'f', 1, 64)
	if strings.HasPrefix(l.Locale, "pt") {
		text = strings.Replace(text, ".", ",", 1)
	}
	return text
}

// Field is what a person reads in place of an input's name, in a summary of the
// rejected fields: "subject" reads "Assunto". A field with no line reads the
// way the input was named.
func (l Labels) Field(name string) string {
	if l.has("field." + name) {
		return l.T("field." + name)
	}
	words := strings.ReplaceAll(name, "_", " ")
	if words == "" {
		return words
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// Summary is one line per rejected message, the field named before it, for the
// banner above a form. Nil when nothing was rejected, so a layout that ranges
// over it draws no banner.
func (l Labels) Summary(errs validation.Errors) []string {
	if len(errs) == 0 {
		return nil
	}
	out := make([]string, 0, len(errs))
	for _, field := range errs.Keys() {
		for _, msg := range errs.Get(field) {
			out = append(out, l.line("validation.summary", translation.Replace{"field": l.Field(field), "message": msg}))
		}
	}
	return out
}

// message is a validation message of this package, in the locale of the
// labels.
func (l Labels) message(key string, replace ...translation.Replace) string {
	var r translation.Replace
	if len(replace) > 0 {
		r = replace[0]
	}
	return l.line("validation."+key, r)
}

// invalid is a rejected input with one message on one field.
func (l Labels) invalid(field, key string, replace ...translation.Replace) validation.Errors {
	return validation.Errors{field: {l.message(key, replace...)}}
}
