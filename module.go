// Package rating is an Arandu module: ratings with stars, reports and
// moderation, for a multi-tenant application.
//
// An account rates a subject -- an application of the catalogue, a plan, a
// partner: the package is generic over a kind and an identifier -- with one to
// five stars and a text if it wants one, and the rating is published at once.
// The owner of the subject reads the ratings it received with their average and
// count, and when it disagrees with one it reports it, with a reason. The
// platform team's moderation decides the report: it removes the rating, corrects
// its text, or archives the report, and keeps who decided, when, why, and the
// rating before and after.
//
// The sides are separate. The author's and the owner's screens belong to the
// customer's panel and share its session; the moderation's belong to the
// operator console, and the team's session is valid there only. Each side has
// its own services, policy, screens and prefix.
//
// The files are laid out by role rather than by layer, so the whole package
// reads top to bottom:
//
//	module.go      -> registration, routes and the contract with the framework
//	config.go      -> what the application passes in, and the route table
//	contracts.go   -> what the application implements: subjects, eligibility, directory, notifier
//	model.go       -> the entities, their own rules, and what they may answer with
//	policy.go      -> who may do what: the author, the owner, the moderation
//	service.go     -> the author's use cases, and what the three services share
//	owner.go       -> the owner's use cases
//	moderation.go  -> the moderation's use cases
//	grants.go      -> the one place a Grant is scoped to the tenant of a row
//	activity.go    -> what is recorded in arandu-activitylog, and what is told
//	handlers.go    -> the controllers of the three sides
//	pages.go       -> what the screens are handed
//	screens.go     -> building it from what a service answered
//	migrations.go  -> the schema
//	views.go       -> the files the application takes ownership of
//
// An application registers it explicitly. There is no service provider, no
// container and no discovery: the wiring is the lines somebody wrote, and
// reading them is how they learn what the application is made of.
package rating

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/foundation"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/view"
)

// Module is what the application registers.
//
// It implements foundation.Module, which is Name and Routes and nothing else --
// that pair is the whole public contract between a package and the framework.
//
// It also implements foundation.Migratable, because it owns tables, and
// foundation.Publishable, because it hands view sources to the project. The
// other optional interfaces are declared beside Module in the framework and are
// opted into the same way, by implementing them: Bootable to prepare state at
// boot, Background to run a loop of its own, Schedulable to declare work for
// the scheduler, Health to report on the storage it depends on, Closable to
// give resources back at shutdown.
type Module struct {
	cfg        Config
	ratings    *RatingService
	owner      *OwnerService
	moderation *ModerationService
	sessions   *security.SessionStore
}

// Compile-time proof that the module honors the contracts it claims.
var (
	_ foundation.Module      = (*Module)(nil)
	_ foundation.Migratable  = (*Module)(nil)
	_ foundation.Bootable    = (*Module)(nil)
	_ foundation.Publishable = (*Module)(nil)
)

// New returns the module, or the reason it cannot be built.
//
// The collaborators are parameters and not fields somebody fills in afterwards:
// a module that could be registered half-wired is a module whose first request
// is the thing that reports the missing half.
//
// It returns an error rather than panicking or carrying on, because everything
// it refuses is a wiring mistake, and a wiring mistake found at boot costs one
// restart. The same mistake found later is a request that reached a nil handle.
func New(cfg Config, db *data.DB, sessions *security.SessionStore) (*Module, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("rating: New needs a database handle: this package owns tables, and there is no in-memory mode that would let it start without one")
	}
	if sessions == nil {
		return nil, errors.New("rating: New needs a session store: every route is mounted behind RequireAuth, which reads it to admit a request and to put who is asking on it")
	}
	cfg = cfg.withDefaults()
	return &Module{
		cfg:        cfg,
		ratings:    NewRatingService(db, cfg),
		owner:      NewOwnerService(db, cfg),
		moderation: NewModerationService(db, cfg),
		sessions:   sessions,
	}, nil
}

// Name is the module identifier: a lowercase slug, stable, no spaces.
//
// It is what `aru route:list` groups by and what the route names are prefixed with,
// so changing it changes addresses that other code has already written down.
func (m *Module) Name() string { return "rating" }

// Ratings is the author's use cases, for an application that calls them from
// its own code: a catalogue that draws the stars of each subject. Every method
// asks the policy, so handing it out opens nothing.
func (m *Module) Ratings() *RatingService { return m.ratings }

// Owner is the owner's use cases, for an application that calls them from its
// own code. Every method asks OwnerPolicy, so handing it out opens nothing.
func (m *Module) Owner() *OwnerService { return m.owner }

// Moderation is the moderation's use cases, for an application that calls them
// from its own code. Every method asks ModerationPolicy, so handing it out
// opens nothing.
func (m *Module) Moderation() *ModerationService { return m.moderation }

// URLs are the addresses of the routes, for an application that links to them:
// a menu, an e-mail, the page of a subject.
func (m *Module) URLs() URLs { return URLs{cfg: m.cfg} }

// Routes registers every route of the module: the customer's and the
// moderation's.
//
// An application that keeps the sides apart -- and the team's session is valid
// on its own side only -- registers them separately with CustomerRoutes and
// TeamRoutes, each on the router group its own guards are on.
//
// Every route needs somebody signed in, so the guard is mounted once per side,
// on the router the routes are registered on, rather than route by route: r is
// replaced by the guarded group before the first registration, and no unguarded
// router is left in scope for the next route to be added to. RequireAuth sends
// a request with no session to the sign-in screen, and puts the subject of one
// that has a session on its context, where a handler reads it with ctx.User;
// Config.Middleware runs after it, and Config.TeamMiddleware after that on the
// moderation's routes.
func (m *Module) Routes(r *fhttp.Router) {
	m.CustomerRoutes(r)
	m.TeamRoutes(r)
}

// CustomerRoutes registers the routes of the customer's panel -- the author's,
// under Prefix, and the owner's, under OwnerPrefix -- for an application that
// mounts them inside its own side with that side's guards on r.
func (m *Module) CustomerRoutes(r *fhttp.Router) { m.register(r, false) }

// TeamRoutes registers the moderation's routes alone, under TeamPrefix, for an
// application that mounts them inside its operator console with that console's
// guards on r.
func (m *Module) TeamRoutes(r *fhttp.Router) { m.register(r, true) }

func (m *Module) register(r *fhttp.Router, team bool) {
	guards := []fhttp.Middleware{middleware.RequireAuth(m.sessions)}
	for _, mw := range m.cfg.Middleware {
		guards = append(guards, mw)
	}
	if team {
		for _, mw := range m.cfg.TeamMiddleware {
			guards = append(guards, mw)
		}
	}
	r = r.Group("", guards...)
	handlers := m.handlers()
	for _, rt := range routeTable(m.cfg) {
		if rt.customer() == team {
			continue
		}
		r.Action(rt.method, rt.path, handlers[rt.name]).Name(rt.name)
	}
}

// screen builds the page of a screen: the title, the application's name and the
// CSRF token view.New reads off the request, the sentences, and the chrome the
// application draws around it.
func (m *Module) screen(ctx *fhttp.Context, who security.Subject, titleKey string, team bool) (Screen, error) {
	labels := m.Labels()
	title := labels.T(titleKey)
	page := view.New(ctx, title)
	page.Title = m.documentTitle(title, page.AppName)
	page.Authenticated = who.ID != ""
	sc := Screen{Page: page, Labels: labels, URLs: m.URLs(), Location: m.cfg.Location}
	if m.cfg.Chrome != nil {
		chrome, err := m.cfg.Chrome(ctx, ChromeRequest{Title: title, Team: team, Subject: who})
		if err != nil {
			return Screen{}, err
		}
		sc.chrome = chrome
	}
	return sc, nil
}

// documentTitle is what the browser tab shows for a screen: its title, and the
// application's name after Config.TitleSeparator when both are there.
func (m *Module) documentTitle(title, appName string) string {
	if m.cfg.TitleSeparator == "" || strings.TrimSpace(appName) == "" {
		return title
	}
	return title + m.cfg.TitleSeparator + appName
}

// PublishCommand is what an application runs to take ownership of the views
// this package offers.
//
// It is spelled out as a constant so that whatever says it -- the refusal
// below, or a message an application writes for its own operators -- says one
// thing. A person who is told two different commands for one job tries both.
//
// The command reads the modules the application registered and writes what each
// one declares, which is why it is the application's command and not this
// package's: nothing outside the application knows which modules it holds.
const PublishCommand = "aru vendor:publish --apply"

// Boot refuses to serve when a view this package renders is not in the binary.
//
// A compiled view registers itself from init(), so by the time anything boots
// the question has one answer already: either the application published the
// files, compiled them and imported the package they became, or it did not.
// Asking here turns "did anybody run the install command" into one refusal at
// start-up that names the views and the command, instead of a 500 on the first
// request that reached one of them -- which is where it used to be answered,
// once per page, to whoever happened to open it.
//
// It also holds the destination. Every file the archive offers has to land
// under the module directory named after this module: an archive that reached
// resources/views/home.kyse.go would land on a page the application wrote, and
// what publishes the files writes what the archive says.
func (m *Module) Boot(context.Context) error {
	prefix := viewRoot + "/" + moduleDir + "/" + m.Name() + "/"
	var stray []string
	for _, path := range PublishedPaths() {
		if !strings.HasPrefix(path, prefix) {
			stray = append(stray, path)
		}
	}
	if len(stray) > 0 {
		return fmt.Errorf("rating: %s would be published outside %s, where it lands on a file the application wrote",
			strings.Join(stray, ", "), prefix)
	}

	registered := make(map[string]bool)
	for _, name := range view.Registered() {
		registered[name] = true
	}
	var missing []string
	for _, name := range ViewNames() {
		if !registered[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		// The compiled packages are named because the import is the half of the
		// install nothing else can do: the command writes the sources, the view
		// compiler turns them into Go, and a package the application does not
		// import is not linked at all.
		imports := make([]string, 0, len(ViewPackages()))
		for _, pkg := range ViewPackages() {
			imports = append(imports, "<module path>/"+pkg)
		}
		return fmt.Errorf("rating: no view is registered as %s. Run `%s`, then `aru view:build`, then import %s in bootstrap/app.go",
			strings.Join(missing, ", "), PublishCommand, strings.Join(imports, ", "))
	}
	return nil
}
