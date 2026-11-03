package feature_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	hedatabase "github.com/arandu-io/hesape/database"
	sqliteconnector "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/database/migrations"
	activitylog "github.com/hyz-is/arandu-activitylog"

	rating "github.com/hyz-is/arandu-rating"
)

// dsn opens a database file the way the tests need it: the write lock taken
// when a transaction begins, readers beside a writer, and a wait instead of an
// error when the lock is held.
const dsn = "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(20000)"

// appKey is the key a session store is built over. Any thirty-two bytes will
// do here; a real application reads its own from the environment.
const appKey = "0123456789abcdef0123456789abcdef"

// realm is the tenant the platform team signs in to.
const realm = "platform"

// The subjects of the catalogue the tests rate. Globex owns two applications
// and Initech a plan; Acme and Hooli are the accounts that rate them.
var (
	camera = rating.Ref{Type: "application", ID: "42"}
	alarm  = rating.Ref{Type: "application", ID: "43"}
	plan   = rating.Ref{Type: "plan", ID: "9"}
	ghost  = rating.Ref{Type: "application", ID: "404"}
)

// signedIn are the subjects the tests started sessions for, by tenant and
// identifier: what a session store's resolver answers, as an application's
// reads the account's roles and actions again on every request.
var signedIn sync.Map

// resolveSubjects is the session store's resolver for these tests.
func resolveSubjects(_ context.Context, who security.Subject) (security.Subject, error) {
	if s, ok := signedIn.Load(who.Tenant + "/" + who.ID); ok {
		return s.(security.Subject), nil
	}
	return who, nil
}

// remember makes a subject the one the resolver answers for its account.
func remember(s security.Subject) security.Subject {
	signedIn.Store(s.Tenant+"/"+s.ID, s)
	return s
}

// notifications records what the Notifier was told, and can be made to fail.
type notifications struct {
	mu     sync.Mutex
	events []rating.Event
	fail   error
}

func (n *notifications) Notify(_ context.Context, e rating.Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, e)
	return n.fail
}

func (n *notifications) all() []rating.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]rating.Event(nil), n.events...)
}

// catalogue is the application's Subjects: a shelf of subjects and who owns
// them.
type catalogue map[rating.Ref]rating.Subject

func (c catalogue) Subject(_ context.Context, ref rating.Ref) (rating.Subject, error) {
	if subject, ok := c[ref]; ok {
		return subject, nil
	}
	return rating.Subject{}, rating.ErrNoSubject
}

func (c catalogue) Owned(_ context.Context, tenant string) ([]rating.Subject, error) {
	var out []rating.Subject
	for ref, subject := range c {
		if subject.OwnerTenant == tenant {
			subject.Ref = ref
			out = append(out, subject)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.String() < out[j].Ref.String() })
	return out, nil
}

func theCatalogue() catalogue {
	return catalogue{
		camera: {Name: "Câmera Norte", URL: "/catalogo/42", OwnerTenant: "globex"},
		alarm:  {Name: "Alarme Sul", URL: "javascript:alert(1)", OwnerTenant: "globex"},
		plan:   {Name: "Plano Pro", OwnerTenant: "initech"},
	}
}

// eligibility is the application's Eligibility: everybody may rate, except what
// the test says they may not.
type eligibility struct {
	mu     sync.Mutex
	denied map[string]bool
	asked  []string
}

func (e *eligibility) deny(tenant, user string, ref rating.Ref) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.denied == nil {
		e.denied = map[string]bool{}
	}
	e.denied[tenant+"/"+user+"/"+ref.String()] = true
}

func (e *eligibility) CanRate(_ context.Context, tenant, user string, ref rating.Ref) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.asked = append(e.asked, tenant+"/"+user+"/"+ref.String())
	return !e.denied[tenant+"/"+user+"/"+ref.String()], nil
}

// directory is the application's Directory.
type directory map[string]rating.Person

func (d directory) Person(_ context.Context, _, id string) (rating.Person, error) {
	if p, ok := d[id]; ok {
		return p, nil
	}
	return rating.Person{}, rating.ErrNoPerson
}

// world is one application with this package installed over a real database.
type world struct {
	sql        *sql.DB
	conn       *hedatabase.Connection
	db         *data.DB
	sessions   *security.SessionStore
	module     *rating.Module
	ratings    *rating.RatingService
	owner      *rating.OwnerService
	moderation *rating.ModerationService
	notices    *notifications
	eligible   *eligibility
	logs       *syncBuffer
	clock      *clock
}

// clock is a time the tests move by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Second)
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// install opens a fresh SQLite database, applies the migrations of the log and
// of this package, and builds the module over it.
func install(t *testing.T) world {
	t.Helper()
	return installWith(t, nil)
}

// installWith is install with the module's configuration changed first.
func installWith(t *testing.T, change func(*rating.Config)) world {
	t.Helper()

	path := filepath.Join(t.TempDir(), "rating.sqlite")
	handle, err := sql.Open(sqliteconnector.SQLiteConnector{}.DriverName(), "file:"+path+dsn)
	if err != nil {
		t.Fatalf("opening SQLite: %v", err)
	}
	handle.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = handle.Close() })

	connection := hedatabase.NewConnection(handle, path, "", map[string]any{
		"driver": string(hedatabase.DialectSQLite),
		"name":   "rating-test",
	})
	db := data.Wrap(handle, data.DialectSQLite)
	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	sessions.ResolveSubjects(resolveSubjects)

	activity, err := activitylog.New(activitylog.Config{Tenant: realm, PlatformPolicy: nil}, db, sessions)
	if err != nil {
		t.Fatalf("building arandu-activitylog: %v", err)
	}

	notices := &notifications{}
	eligible := &eligibility{}
	logs := &syncBuffer{}
	held := &clock{now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	cfg := rating.Config{
		TeamTenant:  realm,
		Subjects:    theCatalogue(),
		Eligibility: eligible,
		Directory: directory{
			"user-ana": {ID: "user-ana", Name: "Ana Souza"}, "user-bia": {ID: "user-bia", Name: "Bia Lima"},
			"user-hu": {ID: "user-hu", Name: "Hugo Reis"}, "user-gil": {ID: "user-gil", Name: "Gil Lopes"},
			"mod-1": {ID: "mod-1", Name: "Rita"}, "mod-2": {ID: "mod-2", Name: "Davi"},
		},
		Notifier: notices,
		Activity: activity.Logger(),
		Now:      held.Now,
		Logger:   slog.New(slog.NewTextHandler(logs, nil)),
	}
	if change != nil {
		change(&cfg)
	}
	module, err := rating.New(cfg, db, sessions)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}
	migrations := hedatabase.ForMigrations(connection)
	for _, migration := range append(activity.Migrations(), module.Migrations()...) {
		if err := migration.Up(context.Background(), migrations); err != nil {
			t.Fatalf("applying %s: %v", migration.GetName(), err)
		}
	}
	return world{
		sql: handle, conn: connection, db: db, sessions: sessions, module: module,
		ratings: module.Ratings(), owner: module.Owner(), moderation: module.Moderation(),
		notices: notices, eligible: eligible, logs: logs, clock: held,
	}
}

// The subjects of the tests. Roles are written on none of them: a role opens
// nothing, and the policies read the actions.

// author is a member of a space who rates and keeps their own ratings.
func author(tenant, id string) security.Subject {
	return security.Subject{ID: id, Tenant: tenant, Verified: true, Actions: []security.Action{
		rating.RatingCreate, rating.RatingViewOwn, rating.RatingUpdate, rating.RatingDelete, rating.RatingViewSummary,
	}}
}

// manager is a member who also reads and manages the whole space's ratings.
func manager(tenant, id string) security.Subject {
	s := author(tenant, id)
	s.Actions = append(s.Actions, rating.RatingViewSpace, rating.RatingManageSpace)
	return s
}

// owner is a member of the space that owns a subject: it reads what was said of
// it and reports what it disagrees with.
func owner(tenant, id string) security.Subject {
	return security.Subject{ID: id, Tenant: tenant, Verified: true, Actions: []security.Action{
		rating.RatingViewSubject, rating.RatingReport, rating.RatingViewSummary,
	}}
}

// moderator is a member of the platform team with every action of the team.
func moderator(id string) security.Subject {
	return security.Subject{ID: id, Tenant: realm, Verified: true, Roles: []string{"admin"}, Actions: rating.TeamActions()}
}

// rate rates a subject as the member and fails the test if it cannot.
func rate(t *testing.T, w world, who security.Subject, ref rating.Ref, score int, body string) *rating.Rating {
	t.Helper()
	record, err := w.ratings.Create(context.Background(), who, ref, rating.RateRequest{Score: score, Body: body})
	if err != nil {
		t.Fatalf("rating %s: %v", ref, err)
	}
	return record
}

// report reports a rating as the owner and fails the test if it cannot.
func report(t *testing.T, w world, who security.Subject, ratingID, reason string) *rating.Report {
	t.Helper()
	filed, err := w.owner.Report(context.Background(), who, ratingID, rating.ReportRequest{Reason: reason})
	if err != nil {
		t.Fatalf("reporting %s: %v", ratingID, err)
	}
	return filed
}

// requireErr fails unless err wraps want.
func requireErr(t *testing.T, err, want error, what string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: got %v, want %v", what, err, want)
	}
}

// entry is a row of the activity log, as the tests read it.
type entry struct {
	Event, Description, Causer, Subject, Properties string
}

// entries are the rows the log holds for a tenant, oldest first. They are read
// with a statement of the test's own: the log's policy denies reading by
// default, and what these tests ask is only what was written.
func entries(t *testing.T, w world, tenant string) []entry {
	t.Helper()
	rows, err := w.sql.Query(`SELECT COALESCE(event, ''), description, COALESCE(causer_id, ''), COALESCE(subject_id, ''), COALESCE(properties, '')
		FROM activity_log WHERE tenant_id = ? ORDER BY created_at, id`, tenant)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	defer rows.Close()
	var out []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.Event, &e.Description, &e.Causer, &e.Subject, &e.Properties); err != nil {
			t.Fatalf("reading a log row: %v", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return out
}

// events are the names of the entries of a tenant.
func events(t *testing.T, w world, tenant string) []string {
	t.Helper()
	var out []string
	for _, e := range entries(t, w, tenant) {
		out = append(out, e.Event)
	}
	return out
}

// syncBuffer is a log the tests read what the package could not return.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// hedatabaseFor is the connection a migration is run on.
func hedatabaseFor(w world) migrations.Connection { return hedatabase.ForMigrations(w.conn) }
