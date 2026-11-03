package rating

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/translation"
	activitylog "github.com/hyz-is/arandu-activitylog"
)

// The defaults for the optional settings. They are constants rather than
// literals inside Config.withDefaults, so the value a reader finds here is the
// value the package uses.
const (
	// DefaultPrefix is where the author's routes are mounted when Config leaves
	// Prefix empty.
	DefaultPrefix = "/ratings"
	// DefaultOwnerPrefix is where the owner's routes are mounted when Config
	// leaves OwnerPrefix empty.
	DefaultOwnerPrefix = "/ratings/received"
	// DefaultTeamPrefix is where the moderation's routes are mounted when
	// Config leaves TeamPrefix empty.
	DefaultTeamPrefix = "/admin/ratings"
	// DefaultPageSize is how many rows one page of a list answers with when
	// Config leaves PageSize at zero.
	DefaultPageSize = 25
	// MaxPageSize is the ceiling PageSize is refused above. A page nobody
	// bounded is a page that reads the whole table on the day the table is
	// large.
	MaxPageSize = 100
	// DefaultLocale is the language of the screens, the messages and the
	// activity descriptions when Config.Locale names none.
	DefaultLocale = "pt-BR"
	// FallbackLocale answers every line the locale in use does not have.
	FallbackLocale = "en"
	// DefaultMaxBodyChars is the longest text of a rating, in characters, when
	// Config leaves MaxBodyChars at zero.
	DefaultMaxBodyChars = 2000
	// MaxBodyCharsLimit is the ceiling MaxBodyChars is refused above. The text
	// comes from people the application does not control, and it is stored,
	// listed and shown to a third party.
	MaxBodyCharsLimit = 10000
	// DefaultMaxReasonChars is the longest reason of a report, in characters,
	// when Config leaves MaxReasonChars at zero.
	DefaultMaxReasonChars = 1000
	// MaxReasonCharsLimit is the ceiling MaxReasonChars is refused above.
	MaxReasonCharsLimit = 5000
	// DefaultMaxNoteChars is the longest justification of a decision, in
	// characters, when Config leaves MaxNoteChars at zero.
	DefaultMaxNoteChars = 1000
	// MaxNoteCharsLimit is the ceiling MaxNoteChars is refused above.
	MaxNoteCharsLimit = 5000
	// MaxEditWindow is the ceiling EditWindow is refused above: past ten years
	// a window is a way of writing "never closes" that a zero says better.
	MaxEditWindow = 10 * 365 * 24 * time.Hour
)

// Config is what the application passes when it wires this package.
//
// A typed struct rather than a map: a misspelled key in a map is a setting that
// silently keeps its default, and the failure shows up as behaviour nobody
// asked for rather than as an error. Here a field that does not exist does not
// compile.
//
// It names no tenant for the customers, and that is deliberate. Every route
// sits behind RequireAuth, so the tenant of every customer statement is the one
// the Grant carries, from the session of whoever is asking; a tenant written
// here would be a second place for one to come from. The one tenant it does
// name is the moderation's own, TeamTenant, because the platform team is not a
// customer.
type Config struct {
	// TeamTenant is the tenant the platform's own team signs in to: the realm of
	// the operator console. A moderator is a subject of this tenant who holds
	// the team's actions, and the team policy refuses any other tenant's
	// subject whatever actions they carry. It comes from the application's own
	// configuration, never from a request. Required.
	TeamTenant string

	// Subjects tells the package what a subject is and who owns it. Required.
	Subjects Subjects

	// Eligibility says whether an account may rate a subject. Required: a
	// package that let anybody rate anything would be one nobody could trust the
	// average of.
	Eligibility Eligibility

	// Prefix is the path the author's routes are mounted under. Empty means
	// DefaultPrefix.
	Prefix string

	// OwnerPrefix is the path the owner's routes are mounted under. It is on
	// the customer's side, beside Prefix. Empty means DefaultOwnerPrefix.
	OwnerPrefix string

	// TeamPrefix is the path the moderation's routes are mounted under. Empty
	// means DefaultTeamPrefix. The sides are separate: the team's session is
	// valid on this side only, and the customer's on the others.
	TeamPrefix string

	// Paths are the segments every route is built from, under the three
	// prefixes. A field left empty keeps its DefaultPaths value; PortuguesePaths
	// is the whole set in Brazilian Portuguese.
	Paths Paths

	// PageSize is how many rows one page of a list answers with. Zero means
	// DefaultPageSize, and anything above MaxPageSize is refused rather than
	// clamped: a number somebody wrote and did not get is worse than a number
	// somebody wrote and was told about.
	PageSize int

	// MaxBodyChars is the longest text of a rating, MaxReasonChars the longest
	// reason of a report and MaxNoteChars the longest justification of a
	// decision, all in characters. Zero means the default of each, and anything
	// above its ceiling is refused.
	MaxBodyChars   int
	MaxReasonChars int
	MaxNoteChars   int

	// RequireBody makes the text of a rating mandatory. The default is that
	// stars alone are a rating.
	RequireBody bool

	// EditWindow is how long after it was written an author may still change a
	// rating. Zero means there is no limit. A rating under review is not
	// editable whatever the window says.
	EditWindow time.Duration

	// ModerationMayChangeScore lets the moderation correct the stars when it
	// alters a rating. The default is that it corrects the text and the author's
	// score stays: a moderator removes an offence, they do not decide what
	// somebody thought of a thing.
	ModerationMayChangeScore bool

	// Middleware runs on every route of this package after RequireAuth, in
	// order, and TeamMiddleware after it on the moderation's routes only. An
	// application whose permissions arandu-permission administers mounts its
	// Roles() in Middleware: RequireAuth puts the subject as the session
	// resolved it, without the actions arandu-permission resolves on each
	// request. A guard of the application's own side -- the operator console's
	// -- goes in TeamMiddleware. A guard answers "is this request on the right
	// side" and stops; the policy still decides.
	Middleware     []func(http.Handler) http.Handler
	TeamMiddleware []func(http.Handler) http.Handler

	// Directory names the people who rate and report. Optional; see Directory.
	Directory Directory

	// Notifier is told when a rating is reported and when a report is decided.
	// Nil tells nobody. See Notifier.
	Notifier Notifier

	// Activity is arandu-activitylog's logger, the application's one instance
	// (activityModule.Logger()). Every act that changes something is recorded
	// under the Grant it was authorized with, in the log named ActivityLogName.
	// Nil records nothing.
	Activity *activitylog.Logger

	// Translator is the application's own catalogue, asked before the one this
	// package ships, key by key: "rating.screen.index_title". Optional.
	Translator *translation.Translator

	// Locale is the language every screen, validation message and activity
	// description of this package is written in. Empty means DefaultLocale. The
	// package ships the locales Locales() lists; any other reads the
	// application's Translator first and English after it.
	Locale string

	// TitleSeparator joins a screen's title to the application's name in the
	// document title: with " — ", the list is titled "Avaliações — Alpr.sh".
	// Empty titles a screen with its own title alone.
	TitleSeparator string

	// Chrome is what the application's layout draws around a screen of this
	// package: its sidebar, its navigation, the account menu. It is called once
	// per screen, after RequireAuth and before the page is rendered, and what it
	// answers is carried on the page as Screen.Chrome, where a layout asserts
	// the interface it draws from. An error is returned as the handler's. Nil
	// draws the screens with the page hesape's view.New builds and nothing
	// more.
	Chrome func(ctx *fhttp.Context, req ChromeRequest) (any, error)

	// Location is the timezone an instant is written in on the screens. Nil
	// means DefaultLocation.
	Location *time.Location

	// Now is the clock: every timestamp a row carries is read from it. Nil
	// means time.Now. A test holds it still; an application leaves it nil.
	Now func() time.Time

	// Logger receives what the package cannot return to anybody: a Notifier
	// that failed after the change was stored. Nil means slog.Default().
	Logger *slog.Logger
}

// ChromeRequest is what Config.Chrome is asked about one screen.
type ChromeRequest struct {
	// Title is the screen's title, already in the screen's language, without
	// the application's name Config.TitleSeparator adds to the document title.
	Title string
	// Team says the screen is one of the moderation's, mounted under
	// TeamPrefix.
	Team bool
	// Subject is who is asking, as RequireAuth and Config.Middleware left it.
	Subject security.Subject
}

// Paths are the segments the routes of this package are built from. Each one
// starts with "/" and holds no wildcard.
//
//	GET  Prefix                                       the account's ratings
//	GET  Prefix + Subject + /{type}/{id}              the form that rates a subject
//	POST Prefix + Subject + /{type}/{id}              rate it
//	GET  Prefix + /{id}                               one rating
//	POST Prefix + /{id} + Edit                        change it
//	POST Prefix + /{id} + Withdrawal                  delete it
//
//	GET  OwnerPrefix                                  the subjects the account owns
//	GET  OwnerPrefix + Subject + /{type}/{id}         the ratings of one
//	GET  OwnerPrefix + Reports                        the reports the account filed
//	GET  OwnerPrefix + Reports + /{rating}            the form that reports a rating
//	POST OwnerPrefix + Reports + /{rating}            report it
//
//	GET  TeamPrefix                                   the queue of reports
//	GET  TeamPrefix + /{id}                           one report
//	POST TeamPrefix + /{id} + Decision                decide it
type Paths struct {
	Subject, Edit, Withdrawal, Reports, Decision string
}

// DefaultPaths are the English segments.
var DefaultPaths = Paths{
	Subject: "/subject", Edit: "/edit", Withdrawal: "/withdrawal", Reports: "/reports", Decision: "/decision",
}

// PortuguesePaths are the segments in Brazilian Portuguese, for an application
// whose addresses are written in it. Mounted at "/painel/avaliacoes",
// "/painel/avaliacoes-recebidas" and "/admin/avaliacoes" they read
// /painel/avaliacoes/assunto/aplicacao/42, /painel/avaliacoes-recebidas/denuncias
// and /admin/avaliacoes/{id}/decisao.
var PortuguesePaths = Paths{
	Subject: "/assunto", Edit: "/edicao", Withdrawal: "/retirada", Reports: "/denuncias", Decision: "/decisao",
}

// fields are the segments with their names, for validating and filling them.
func (p *Paths) fields() []struct {
	name  string
	value *string
} {
	return []struct {
		name  string
		value *string
	}{
		{"Subject", &p.Subject}, {"Edit", &p.Edit}, {"Withdrawal", &p.Withdrawal}, {"Reports", &p.Reports},
		{"Decision", &p.Decision},
	}
}

// validate reports a segment that cannot be part of a route.
func (p Paths) validate() error {
	for _, f := range p.fields() {
		v := *f.value
		if v == "" {
			continue
		}
		if v[0] != '/' || strings.ContainsAny(v, "{} \t?#") || v == "/" || strings.HasSuffix(v, "/") {
			return fmt.Errorf("rating: Config.Paths.%s is %q: a segment starts with /, ends without one and holds no wildcard", f.name, v)
		}
	}
	return nil
}

// withDefaults fills every empty segment from DefaultPaths.
func (p Paths) withDefaults() Paths {
	d := DefaultPaths
	defaults := d.fields()
	for i, f := range p.fields() {
		if *f.value == "" {
			*f.value = *defaults[i].value
		}
	}
	return p
}

// DefaultLocation is the timezone the screens write instants in when Config
// leaves Location nil. The package is Brazilian first; an application elsewhere
// sets its own.
const DefaultLocation = "America/Sao_Paulo"

// reservedPrefix is the namespace the framework keeps for itself.
const reservedPrefix = "/_arandu"

// Validate reports what the configuration cannot be used with.
//
// It is called by New, so an application with a setting that cannot work fails
// where it is wired rather than on the first request that needed it.
func (c Config) Validate() error {
	if !security.ValidTenant(c.TeamTenant) {
		return fmt.Errorf("rating: Config.TeamTenant is %q, which cannot be a tenant: pass the tenant the platform team signs in to (lowercase letters, digits, - and _, up to 64 characters)", c.TeamTenant)
	}
	if c.Subjects == nil {
		return fmt.Errorf("rating: Config.Subjects is nil: the package cannot tell what a subject is or who owns it, and the owner's reading and reporting both depend on that")
	}
	if c.Eligibility == nil {
		return fmt.Errorf("rating: Config.Eligibility is nil: say who may rate what, even if the answer is everybody")
	}
	for _, prefix := range []struct{ name, value string }{{"Prefix", c.Prefix}, {"OwnerPrefix", c.OwnerPrefix}, {"TeamPrefix", c.TeamPrefix}} {
		if prefix.value == "" {
			continue
		}
		if prefix.value[0] != '/' || (len(prefix.value) > 1 && strings.HasSuffix(prefix.value, "/")) {
			return fmt.Errorf("rating: Config.%s is %q and has to start with / and end without one", prefix.name, prefix.value)
		}
		if prefix.value == reservedPrefix || strings.HasPrefix(prefix.value, reservedPrefix+"/") {
			return fmt.Errorf("rating: Config.%s is %q, under %s/, which the framework keeps for itself", prefix.name, prefix.value, reservedPrefix)
		}
	}
	if err := c.Paths.validate(); err != nil {
		return err
	}
	for i, mw := range c.Middleware {
		if mw == nil {
			return fmt.Errorf("rating: Config.Middleware[%d] is nil", i)
		}
	}
	for i, mw := range c.TeamMiddleware {
		if mw == nil {
			return fmt.Errorf("rating: Config.TeamMiddleware[%d] is nil", i)
		}
	}
	if c.PageSize < 0 || c.PageSize > MaxPageSize {
		return fmt.Errorf("rating: Config.PageSize is %d, and has to be between 0 and %d, where 0 means %d", c.PageSize, MaxPageSize, DefaultPageSize)
	}
	for _, limit := range []struct {
		name         string
		value        int
		ceiling, def int
	}{
		{"MaxBodyChars", c.MaxBodyChars, MaxBodyCharsLimit, DefaultMaxBodyChars},
		{"MaxReasonChars", c.MaxReasonChars, MaxReasonCharsLimit, DefaultMaxReasonChars},
		{"MaxNoteChars", c.MaxNoteChars, MaxNoteCharsLimit, DefaultMaxNoteChars},
	} {
		if limit.value < 0 || limit.value > limit.ceiling {
			return fmt.Errorf("rating: Config.%s is %d, and has to be between 0 and %d, where 0 means %d", limit.name, limit.value, limit.ceiling, limit.def)
		}
	}
	if c.EditWindow < 0 || c.EditWindow > MaxEditWindow {
		return fmt.Errorf("rating: Config.EditWindow is %s, and has to be between 0 and %s, where 0 means an author may always edit", c.EditWindow, MaxEditWindow)
	}
	if c.Prefix != "" && c.Prefix == c.OwnerPrefix || c.Prefix != "" && c.Prefix == c.TeamPrefix || c.OwnerPrefix != "" && c.OwnerPrefix == c.TeamPrefix {
		return fmt.Errorf("rating: Config.Prefix, Config.OwnerPrefix and Config.TeamPrefix have to be three different paths")
	}
	return validateRoutes(c.withDefaults())
}

// validateRoutes asks the standard library to parse the exact patterns the
// module will register, every one of them. Its parser is not exported and
// reports an invalid pattern, or two that collide, by panic, so the throwaway
// mux turns that boot-time panic into the configuration error New promises.
func validateRoutes(c Config) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("rating: Config.Prefix %q, Config.OwnerPrefix %q, Config.TeamPrefix %q and Config.Paths cannot be registered as routes: %v",
				c.Prefix, c.OwnerPrefix, c.TeamPrefix, recovered)
		}
	}()

	handler := http.NotFoundHandler()
	mux := http.NewServeMux()
	for _, route := range routeTable(c) {
		mux.Handle(route.method+" "+route.path, handler)
	}
	return nil
}

// withDefaults returns the configuration with the optional fields filled in.
//
// It runs after Validate and never before: filling a default in first would
// hide the value somebody actually wrote from the check that would have refused
// it.
func (c Config) withDefaults() Config {
	if c.Prefix == "" {
		c.Prefix = DefaultPrefix
	}
	if c.OwnerPrefix == "" {
		c.OwnerPrefix = DefaultOwnerPrefix
	}
	if c.TeamPrefix == "" {
		c.TeamPrefix = DefaultTeamPrefix
	}
	c.Paths = c.Paths.withDefaults()
	if c.PageSize == 0 {
		c.PageSize = DefaultPageSize
	}
	if c.MaxBodyChars == 0 {
		c.MaxBodyChars = DefaultMaxBodyChars
	}
	if c.MaxReasonChars == 0 {
		c.MaxReasonChars = DefaultMaxReasonChars
	}
	if c.MaxNoteChars == 0 {
		c.MaxNoteChars = DefaultMaxNoteChars
	}
	if c.Locale == "" {
		c.Locale = DefaultLocale
	}
	if c.Location == nil {
		if loc, err := time.LoadLocation(DefaultLocation); err == nil {
			c.Location = loc
		} else {
			c.Location = time.FixedZone("BRT", -3*60*60)
		}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// side is which of the three sides a route belongs to.
type side int

const (
	sideAuthor side = iota
	sideOwner
	sideTeam
)

// route is one line of the table this module registers.
type route struct {
	name   string
	method string
	path   string
	side   side
}

// customer says the route is on the customer's panel: the author's and the
// owner's, which share a session.
func (r route) customer() bool { return r.side != sideTeam }

// routeTable is the whole table, built from the prefixes and the segments. It
// is read by validateRoutes, which registers it on a throwaway mux, and by the
// router.
func routeTable(c Config) []route {
	p, o, t, s := c.Prefix, c.OwnerPrefix, c.TeamPrefix, c.Paths
	return []route{
		{"rating.index", http.MethodGet, p, sideAuthor},
		{"rating.create", http.MethodGet, p + s.Subject + "/{type}/{id}", sideAuthor},
		{"rating.store", http.MethodPost, p + s.Subject + "/{type}/{id}", sideAuthor},
		{"rating.show", http.MethodGet, p + "/{id}", sideAuthor},
		{"rating.update", http.MethodPost, p + "/{id}" + s.Edit, sideAuthor},
		{"rating.withdraw", http.MethodPost, p + "/{id}" + s.Withdrawal, sideAuthor},

		{"rating.owner.index", http.MethodGet, o, sideOwner},
		{"rating.owner.subject", http.MethodGet, o + s.Subject + "/{type}/{id}", sideOwner},
		{"rating.owner.reports", http.MethodGet, o + s.Reports, sideOwner},
		{"rating.owner.report_form", http.MethodGet, o + s.Reports + "/{rating}", sideOwner},
		{"rating.owner.report", http.MethodPost, o + s.Reports + "/{rating}", sideOwner},

		{"rating.team.index", http.MethodGet, t, sideTeam},
		{"rating.team.show", http.MethodGet, t + "/{id}", sideTeam},
		{"rating.team.decide", http.MethodPost, t + "/{id}" + s.Decision, sideTeam},
	}
}
