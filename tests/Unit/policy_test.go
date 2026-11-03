package unit_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"

	rating "github.com/hyz-is/arandu-rating"
)

// The properties this package exists to keep are checked here, and they are
// checked against the code rather than described in a document:
//
//  1. the policies deny every action, and open one only for the subject who was
//     granted it, in the tenant the record is in;
//  2. the services authorize before constructing or running the generated query;
//  3. the tenant comes from the Grant;
//  4. nothing reaches the database without passing through the first two.
//
// The fourth is checked by the handle these tests pass in. It wraps a nil
// *sql.DB, so any statement that were issued would panic and fail the test
// loudly -- which makes "the refusal happened before the Model" a fact the
// suite proves rather than a comment. The structural twin in audit_test.go
// keeps that order visible on every service method, including an allowed path.

// customerActions are the actions of the customer's sides, and teamActions the
// moderation's: every one the three policies answer about.
var (
	customerActions = rating.Actions()
	teamActions     = rating.TeamActions()
	everyAction     = append(append([]security.Action(nil), customerActions...), teamActions...)
)

// administrator is the most privileged subject an application could produce
// without granting it an action. It is the one to test the default with: a
// policy that refuses an administrator who holds no action refuses everyone.
func administrator(tenant string) security.Subject {
	return security.Subject{ID: "user-1", Tenant: tenant, Roles: []string{"admin", "owner", "rating.admin"}, Verified: true}
}

// granted is a subject who holds exactly these actions.
func granted(tenant, id string, actions ...security.Action) security.Subject {
	return security.Subject{ID: id, Tenant: tenant, Verified: true, Actions: actions}
}

func TestThePoliciesDenyEveryActionByDefault(t *testing.T) {
	t.Parallel()

	for _, action := range everyAction {
		for tenant := range map[string]bool{"acme": true, "platform": true} {
			who := administrator(tenant)
			ctx := context.Background()
			if _, err := security.Authorize(ctx, rating.RatingPolicy{}, who, action, rating.Rating{}); !errors.Is(err, security.ErrForbidden) {
				t.Errorf("the author's policy allowed %s to an administrator of %s who was granted nothing: %v", action, tenant, err)
			}
			if _, err := security.Authorize(ctx, rating.OwnerPolicy{}, who, action, rating.Target{}); !errors.Is(err, security.ErrForbidden) {
				t.Errorf("the owner's policy allowed %s to an administrator of %s who was granted nothing: %v", action, tenant, err)
			}
			if _, err := security.Authorize(ctx, rating.ModerationPolicy{Realm: "platform"}, who, action, rating.Report{}); !errors.Is(err, security.ErrForbidden) {
				t.Errorf("the moderation's policy allowed %s to an administrator of %s who was granted nothing: %v", action, tenant, err)
			}
		}
	}
}

func TestTheTwoSetsOfActionsAreApart(t *testing.T) {
	t.Parallel()

	for _, action := range customerActions {
		if slices.Contains(teamActions, action) {
			t.Errorf("%s is in both sets, so a catalogue an organization grants from could hold a team action", action)
		}
		if !strings.HasPrefix(string(action), "rating.") {
			t.Errorf("%s is not named after the package", action)
		}
	}
	for _, action := range teamActions {
		if !strings.HasPrefix(string(action), "rating.") {
			t.Errorf("%s is not named after the package", action)
		}
	}
	if len(everyAction) != 11 {
		t.Errorf("%d actions are declared; the release notes name eleven", len(everyAction))
	}
}

func TestAnAuthorIsAllowedWhatTheyWereGrantedAndNothingMore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	own := rating.Rating{ID: "r-1", TenantID: "acme", AuthorID: "user-ana"}
	others := rating.Rating{ID: "r-2", TenantID: "acme", AuthorID: "user-bia"}
	policy := rating.RatingPolicy{}

	for _, c := range []struct {
		name   string
		who    security.Subject
		action security.Action
		on     rating.Rating
		want   bool
	}{
		{"create, granted", granted("acme", "user-ana", rating.RatingCreate), rating.RatingCreate, rating.Rating{}, true},
		{"create, not granted", granted("acme", "user-ana", rating.RatingViewOwn), rating.RatingCreate, rating.Rating{}, false},
		{"create, on a stored rating", granted("acme", "user-ana", rating.RatingCreate), rating.RatingCreate, own, false},
		{"view own, the probe", granted("acme", "user-ana", rating.RatingViewOwn), rating.RatingViewOwn, rating.Rating{}, true},
		{"view own, theirs", granted("acme", "user-ana", rating.RatingViewOwn), rating.RatingViewOwn, own, true},
		{"view own, somebody else's", granted("acme", "user-ana", rating.RatingViewOwn), rating.RatingViewOwn, others, false},
		{"view own, another space's", granted("acme", "user-ana", rating.RatingViewOwn), rating.RatingViewOwn, rating.Rating{ID: "r-3", TenantID: "globex", AuthorID: "user-ana"}, false},
		{"view space, somebody else's", granted("acme", "user-boss", rating.RatingViewSpace), rating.RatingViewSpace, others, true},
		{"view space, another space's", granted("acme", "user-boss", rating.RatingViewSpace), rating.RatingViewSpace, rating.Rating{ID: "r-3", TenantID: "globex"}, false},
		{"update, theirs", granted("acme", "user-ana", rating.RatingUpdate), rating.RatingUpdate, own, true},
		{"update, somebody else's", granted("acme", "user-ana", rating.RatingUpdate), rating.RatingUpdate, others, false},
		{"update, somebody else's, managing the space", granted("acme", "user-boss", rating.RatingUpdate, rating.RatingManageSpace), rating.RatingUpdate, others, true},
		{"update, with only the right to read", granted("acme", "user-ana", rating.RatingViewOwn), rating.RatingUpdate, own, false},
		{"delete, theirs", granted("acme", "user-ana", rating.RatingDelete), rating.RatingDelete, own, true},
		{"delete, somebody else's", granted("acme", "user-ana", rating.RatingDelete), rating.RatingDelete, others, false},
		{"summary", granted("acme", "user-ana", rating.RatingViewSummary), rating.RatingViewSummary, rating.Rating{}, true},
		{"a team action", granted("acme", "user-ana", rating.RatingModerate), rating.RatingModerate, own, false},
		{"an owner's action", granted("acme", "user-ana", rating.RatingReport), rating.RatingReport, own, false},
	} {
		err := policy.Can(ctx, c.who, c.action, c.on)
		if (err == nil) != c.want {
			t.Errorf("%s: got %v, want allowed=%v", c.name, err, c.want)
		}
	}
}

func TestTheOwnerIsAllowedOnlyWhatIsTheirSubjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	policy := rating.OwnerPolicy{}
	ref := rating.Ref{Type: "application", ID: "42"}
	mine := rating.Target{Subject: ref, Owner: "acme"}
	theirs := rating.Target{Subject: ref, Owner: "globex"}
	unowned := rating.Target{Subject: ref}

	for _, c := range []struct {
		name   string
		who    security.Subject
		action security.Action
		on     rating.Target
		want   bool
	}{
		{"view, the probe", granted("acme", "u", rating.RatingViewSubject), rating.RatingViewSubject, rating.Target{}, true},
		{"view, a subject they own", granted("acme", "u", rating.RatingViewSubject), rating.RatingViewSubject, mine, true},
		{"view, a subject another space owns", granted("acme", "u", rating.RatingViewSubject), rating.RatingViewSubject, theirs, false},
		{"view, a subject nobody owns", granted("acme", "u", rating.RatingViewSubject), rating.RatingViewSubject, unowned, false},
		{"report, a subject they own", granted("acme", "u", rating.RatingReport), rating.RatingReport, mine, true},
		{"report, a subject another space owns", granted("acme", "u", rating.RatingReport), rating.RatingReport, theirs, false},
		{"report, not granted", granted("acme", "u", rating.RatingViewSubject), rating.RatingReport, mine, false},
		{"view does not open report", granted("acme", "u", rating.RatingViewSubject), rating.RatingReport, rating.Target{}, false},
		{"moderate", granted("acme", "u", rating.RatingModerate), rating.RatingModerate, mine, false},
	} {
		err := policy.Can(ctx, c.who, c.action, c.on)
		if (err == nil) != c.want {
			t.Errorf("%s: got %v, want allowed=%v", c.name, err, c.want)
		}
	}
	// And the refusal about another tenant names it, because it is the one that
	// has to survive somebody opening the actions below it.
	err := policy.Can(ctx, granted("acme", "u", rating.RatingViewSubject), rating.RatingViewSubject, theirs)
	if err == nil || !strings.Contains(err.Error(), "another tenant") {
		t.Errorf("the refusal did not name the tenant: %v", err)
	}
}

func TestThePolicyDeniesARecordOfAnotherTenant(t *testing.T) {
	t.Parallel()

	other := rating.Rating{ID: "record-1", TenantID: "globex", AuthorID: "user-1"}
	everything := granted("acme", "user-1", customerActions...)

	for _, action := range customerActions {
		err := rating.RatingPolicy{}.Can(context.Background(), everything, action, other)
		if err == nil {
			t.Fatalf("the policy allowed %s on a record belonging to another tenant", action)
		}
		// The message is asserted because the tenant check is the one refusal
		// that has to survive somebody opening the actions below it.
		if !strings.Contains(err.Error(), "another tenant") {
			t.Fatalf("the refusal did not name the tenant: %v", err)
		}
	}
}

func TestThePoliciesDenyAGuestAndNobody(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, action := range everyAction {
		for name, run := range map[string]func(who security.Subject) error{
			"author": func(who security.Subject) error {
				_, err := security.Authorize(ctx, rating.RatingPolicy{}, who, action, rating.Rating{})
				return err
			},
			"owner": func(who security.Subject) error {
				_, err := security.Authorize(ctx, rating.OwnerPolicy{}, who, action, rating.Target{})
				return err
			},
			"moderation": func(who security.Subject) error {
				_, err := security.Authorize(ctx, rating.ModerationPolicy{Realm: "acme"}, who, action, rating.Report{})
				return err
			},
		} {
			if err := run(security.Guest("acme")); !errors.Is(err, security.ErrForbidden) {
				t.Fatalf("the %s policy allowed a guest %s: %v", name, action, err)
			}
			// The zero Subject is a session that failed to load, not an
			// anonymous reader, and it is refused before the policy is consulted.
			if err := run(security.Subject{}); !errors.Is(err, security.ErrForbidden) {
				t.Fatalf("the %s policy allowed the empty subject %s: %v", name, action, err)
			}
		}
	}
}

func TestTheModerationPolicyAllowsTheTeamAndOnlyTheTeam(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	policy := rating.ModerationPolicy{Realm: "platform"}
	inAnyTenant := rating.Report{ID: "p-1", TenantID: "globex", RatingTenant: "acme"}

	// A member of the team reads and acts on a report of any tenant: crossing
	// tenants is what the team does.
	for _, action := range teamActions {
		if err := policy.Can(ctx, granted("platform", "team-1", action), action, inAnyTenant); err != nil {
			t.Errorf("a team member granted %s was refused it: %v", action, err)
		}
		// An action opens itself and no other.
		for _, other := range teamActions {
			if other != action && policy.Can(ctx, granted("platform", "team-1", action), other, inAnyTenant) == nil {
				t.Errorf("%s opened %s", action, other)
			}
		}
	}

	// And the customers' actions are not the team's.
	for _, action := range customerActions {
		if policy.Can(ctx, granted("platform", "team-1", action), action, inAnyTenant) == nil {
			t.Errorf("the moderation policy answered the customer's action %s", action)
		}
	}

	all := granted("platform", "team-1", teamActions...)
	cases := map[string]security.Subject{
		"a subject of another tenant": granted("acme", "team-1", teamActions...),
		"a subject viewing as somebody": func() security.Subject {
			s := all
			s.Impersonator = &security.Subject{ID: "operator"}
			return s
		}(),
	}
	for name, who := range cases {
		if err := policy.Can(ctx, who, rating.RatingViewAll, inAnyTenant); err == nil {
			t.Errorf("the moderation policy allowed %s", name)
		}
	}
	// With no realm, nobody is the team.
	if err := (rating.ModerationPolicy{}).Can(ctx, granted("", "team-1", teamActions...), rating.RatingViewAll, inAnyTenant); err == nil {
		t.Error("a policy with no realm allowed somebody")
	}
}

func TestTheGrantsOfReadingOnlyReadAndWhatWritesWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	author, owner, team := rating.RatingPolicy{}, rating.OwnerPolicy{}, rating.ModerationPolicy{Realm: "platform"}
	for _, c := range []struct {
		issue  func(who security.Subject, a security.Action) (security.Grant, error)
		who    security.Subject
		action security.Action
		writes bool
	}{
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, author, w, a, rating.Rating{})
		}, granted("acme", "u", rating.RatingViewOwn), rating.RatingViewOwn, false},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, author, w, a, rating.Rating{})
		}, granted("acme", "u", rating.RatingViewSpace), rating.RatingViewSpace, false},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, author, w, a, rating.Rating{})
		}, granted("acme", "u", rating.RatingViewSummary), rating.RatingViewSummary, false},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, author, w, a, rating.Rating{})
		}, granted("acme", "u", rating.RatingCreate), rating.RatingCreate, true},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, author, w, a, rating.Rating{})
		}, granted("acme", "u", rating.RatingUpdate), rating.RatingUpdate, true},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, author, w, a, rating.Rating{})
		}, granted("acme", "u", rating.RatingDelete), rating.RatingDelete, true},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, owner, w, a, rating.Target{})
		}, granted("acme", "u", rating.RatingViewSubject), rating.RatingViewSubject, false},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, owner, w, a, rating.Target{})
		}, granted("acme", "u", rating.RatingReport), rating.RatingReport, true},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, team, w, a, rating.Report{})
		}, granted("platform", "u", rating.RatingViewAll), rating.RatingViewAll, false},
		{func(w security.Subject, a security.Action) (security.Grant, error) {
			return security.Authorize(ctx, team, w, a, rating.Report{})
		}, granted("platform", "u", rating.RatingModerate), rating.RatingModerate, true},
	} {
		g, err := c.issue(c.who, c.action)
		if err != nil {
			t.Fatalf("%s: %v", c.action, err)
		}
		// A Grant issued to read cannot write: the database refuses it.
		if got := auth.Writable(g) == nil; got != c.writes {
			t.Errorf("a Grant issued for %s is writable = %v, want %v", c.action, got, c.writes)
		}
	}
}

// nilHandle is a handle over no database.
//
// Any statement issued through it panics, which is what makes these tests
// prove that the refusal came first: a service that reached the Model before
// authorizing would crash here rather than pass.
func nilHandle() *data.DB { return data.Wrap(nil, data.DialectSQLite) }

func TestTheServicesRefuseBeforeReachingTheModel(t *testing.T) {
	t.Parallel()

	// A nil handle makes even construction of Ratings panic, at the
	// GetQueryGrammar the query asks the handle for. This catches moving the
	// generated query constructor -- not only its terminal -- ahead of
	// authorization.
	cfg := wired()
	authors := rating.NewRatingService(nil, cfg)
	owners := rating.NewOwnerService(nil, cfg)
	moderation := rating.NewModerationService(nil, cfg)
	ctx := context.Background()
	nobody := administrator("acme")
	nobodyOfTheTeam := administrator("platform")
	ref := rating.Ref{Type: "application", ID: "42"}
	stars := rating.RateRequest{Score: 4, Body: "bom"}

	for name, err := range map[string]error{
		"List":      second(authors.List(ctx, nobody, rating.ListQuery{})),
		"Find":      second(authors.Find(ctx, nobody, "r-1")),
		"Card":      second(authors.Card(ctx, nobody, ref)),
		"Create":    second(authors.Create(ctx, nobody, ref, stars)),
		"Update":    second(authors.Update(ctx, nobody, "r-1", stars)),
		"Withdraw":  authors.Withdraw(ctx, nobody, "r-1"),
		"Summaries": second(authors.Summaries(ctx, nobody, []rating.Ref{ref})),
		"CanRate":   authors.CanRate(ctx, nobody, ref),

		"owner Subjects": second(owners.Subjects(ctx, nobody, rating.OwnedQuery{})),
		"owner Received": second(owners.Received(ctx, nobody, ref, rating.SubjectQuery{})),
		"owner Rating":   second(owners.Rating(ctx, nobody, "r-1")),
		"owner Report":   second(owners.Report(ctx, nobody, "r-1", rating.ReportRequest{Reason: "falso"})),
		"owner Filed":    second(owners.Filed(ctx, nobody, rating.ReportsQuery{})),

		"moderation List":   second(moderation.List(ctx, nobodyOfTheTeam, rating.ReportsQuery{})),
		"moderation Find":   second(moderation.Find(ctx, nobodyOfTheTeam, "p-1")),
		"moderation Decide": second(moderation.Decide(ctx, nobodyOfTheTeam, "p-1", rating.DecisionRequest{Verdict: rating.VerdictRemove, Note: "ofensivo"})),
	} {
		if !errors.Is(err, security.ErrForbidden) {
			t.Errorf("%s reached the Model before the policy refusal: %v", name, err)
		}
	}
}

// second is the error of a call that answers a value and an error.
func second[T any](_ T, err error) error { return err }

// recordingHandle runs nothing and keeps every statement it is handed, so a
// test reads what the table compiles instead of reading its settings. The table
// keeps its settings to itself; what it writes is the behaviour they decide.
type recordingHandle struct {
	*data.DB
	statements []recordedStatement
}

type recordedStatement struct {
	sql      string
	bindings []any
}

// newRecordingHandle borrows the grammar and the processor of an SQLite handle
// over no database, and answers every statement itself.
func newRecordingHandle() *recordingHandle { return &recordingHandle{DB: nilHandle()} }

func (h *recordingHandle) record(sql string, bindings []any) {
	h.statements = append(h.statements, recordedStatement{sql: sql, bindings: slices.Clone(bindings)})
}

func (h *recordingHandle) Select(_ context.Context, sql string, bindings []any, _ bool) ([]query.Record, error) {
	h.record(sql, bindings)
	return nil, nil
}

func (h *recordingHandle) Insert(_ context.Context, sql string, bindings []any) (bool, error) {
	h.record(sql, bindings)
	return true, nil
}

func (h *recordingHandle) Update(_ context.Context, sql string, bindings []any) (int64, error) {
	h.record(sql, bindings)
	return 1, nil
}

func (h *recordingHandle) Delete(_ context.Context, sql string, bindings []any) (int64, error) {
	h.record(sql, bindings)
	return 1, nil
}

func (h *recordingHandle) Statement(_ context.Context, sql string, bindings []any) (bool, error) {
	h.record(sql, bindings)
	return true, nil
}

// first is the first recorded statement that starts with verb.
func (h *recordingHandle) first(t *testing.T, verb string) recordedStatement {
	t.Helper()
	for _, statement := range h.statements {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(statement.sql)), verb) {
			return statement
		}
	}
	t.Fatalf("no %s statement was issued; recorded %v", verb, h.statements)
	return recordedStatement{}
}

// TestRatingsReturnsAWiredTenantScopedQuery holds what ratingTable declares,
// through what it makes the generated query do: the table it reads, a key the
// application writes and the engine never fills, and a tenant_id taken from the
// Grant on the way in and on the way out.
func TestRatingsReturnsAWiredTenantScopedQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	handle := newRecordingHandle()
	table := rating.Ratings(handle).Base().Table()
	if table.Name() != "ratings" {
		t.Fatalf("Ratings table = %q, want ratings", table.Name())
	}
	if key := table.MorphModel(handle); key.GetKeyName() != "id" || key.GetKeyType() != "string" {
		t.Fatalf("Ratings key is %q of type %q; want id, as text", key.GetKeyName(), key.GetKeyType())
	}

	row, err := rating.Ratings(handle).New()
	if err != nil {
		t.Fatalf("building a row: %v", err)
	}
	if row.Table() != table {
		t.Fatal("Ratings returned an entity whose embedded Model is not wired to its table")
	}

	// The key is application-generated text: the insert carries the one the row
	// was given, and the engine is never asked for one.
	row.ID = "record-1"
	if _, err := row.Save(ctx, security.SystemGrant(rating.RatingCreate, "acme")); err != nil {
		t.Fatalf("saving through the recording handle: %v", err)
	}
	insert := handle.first(t, "insert")
	if !slices.Contains(insert.bindings, any("record-1")) || row.ID != "record-1" {
		t.Fatalf("the insert %q %v does not write the application key, or the row lost it (%q)", insert.sql, insert.bindings, row.ID)
	}
	if !strings.Contains(insert.sql, "tenant_id") || !slices.Contains(insert.bindings, any("acme")) {
		t.Fatalf("the insert %q %v does not stamp tenant_id with the Grant's tenant", insert.sql, insert.bindings)
	}

	// And the tenant column scopes a read by the Grant's tenant, for each of the
	// two tables a customer's statement reaches.
	grant := security.SystemGrant(rating.RatingViewOwn, "acme")
	for name, run := range map[string]func() error{
		"ratings": func() error { _, err := rating.Ratings(handle).WhereKey("record-1").First(ctx, grant); return err },
		"reports": func() error { _, err := rating.Reports(handle).WhereKey("record-1").First(ctx, grant); return err },
	} {
		handle.statements = nil
		if err := run(); err != nil {
			t.Fatalf("reading %s through the recording handle: %v", name, err)
		}
		read := handle.first(t, "select")
		if !strings.Contains(read.sql, "tenant_id") || !slices.Contains(read.bindings, any("acme")) {
			t.Fatalf("the read of %s %q %v is not filtered by tenant_id with the Grant's tenant", name, read.sql, read.bindings)
		}
	}
}

// TestTheMissingRecordIsTheModelsNotFound holds the one link between this
// package's sentinel and the router's answer. The router answers
// model.ErrModelNotFound with 404 and has never heard of ErrNotFound, so the
// sentinel is a 404 only while it wraps the model's.
func TestTheMissingRecordIsTheModelsNotFound(t *testing.T) {
	t.Parallel()

	if !errors.Is(rating.ErrNotFound, model.ErrModelNotFound) {
		t.Fatal("ErrNotFound does not wrap model.ErrModelNotFound, so the router would answer a missing record with 500")
	}
}

func TestASystemGrantWithoutATenantReachesNothing(t *testing.T) {
	t.Parallel()

	// A system grant with no tenant names no customer. The Model refuses it
	// while preparing the query, before the nil handle can issue a statement.
	_, err := rating.Ratings(nilHandle()).WhereKey("record-1").First(
		context.Background(), security.SystemGrant(rating.RatingViewOwn, ""))
	if !errors.Is(err, model.ErrNoTenant) {
		t.Fatalf("a system grant with no tenant returned %v, want ErrNoTenant", err)
	}
}

func TestTheTenantComesFromTheGrant(t *testing.T) {
	t.Parallel()

	g := security.SystemGrant(rating.RatingViewOwn, "acme")
	if got := data.Tenant(g); got != "acme" {
		t.Fatalf("data.Tenant(g) = %q, want %q", got, "acme")
	}
	// And a Grant nobody issued carries no tenant at all, so a statement that
	// took its tenant from anywhere else would be reading rows this Grant does
	// not name.
	if got := data.Tenant(security.Grant{}); got != "" {
		t.Fatalf("the zero Grant carries the tenant %q, want none", got)
	}
}

func TestTheRatingsOwnRules(t *testing.T) {
	t.Parallel()

	now := mustTime("2026-11-03T12:00:00Z")
	later := now.Add(time.Hour)
	published := func() *rating.Rating {
		return &rating.Rating{ID: "r-1", Score: 4, Body: "bom", Status: rating.StatusPublished, CreatedAt: now}
	}

	// A published rating is edited, with the window the application allows.
	r := published()
	if err := r.Revise(5, "ótimo", later, 0); err != nil || r.Score != 5 || r.Body != "ótimo" || r.EditedAt == nil || !r.EditedAt.Equal(later) {
		t.Fatalf("revising: %v, %+v", err, r)
	}
	if r.Revise(0, "x", later, 0) == nil || r.Revise(6, "x", later, 0) == nil {
		t.Error("a score that is not one was taken")
	}
	if published().CanEdit(now.Add(2*time.Hour), time.Hour) {
		t.Error("a rating past its window was editable")
	}
	if !published().CanEdit(now.Add(30*time.Minute), time.Hour) || !published().CanEdit(now.Add(1000*time.Hour), 0) {
		t.Error("a rating inside its window, or with none, was not editable")
	}

	// While a report is open nobody changes it.
	reviewed := published()
	reviewed.UnderReview = true
	if reviewed.CanEdit(now, 0) || reviewed.CanWithdraw() || reviewed.Withdraw(later) == nil || reviewed.Revise(1, "", later, 0) == nil {
		t.Error("a rating under review was changed, or could be")
	}
	if reviewed.CanBeReported() {
		t.Error("a rating already reported can be reported again")
	}

	// Withdrawing takes the text and keeps the row, and the account can rate again.
	w := published()
	if err := w.Withdraw(later); err != nil || w.Status != rating.StatusWithdrawn || w.Body != "" || w.CanBeReported() {
		t.Fatalf("withdrawing: %v, %+v", err, w)
	}
	if w.Withdraw(later) == nil || w.Revise(3, "x", later, 0) == nil {
		t.Error("a withdrawn rating was withdrawn again, or edited")
	}
	if err := w.Renew(2, "voltei", "user-2", "Bia", later); err != nil || w.Status != rating.StatusPublished || w.Score != 2 ||
		w.AuthorID != "user-2" || w.EditedAt != nil || !w.CreatedAt.Equal(later) {
		t.Fatalf("renewing: %v, %+v", err, w)
	}
	if published().Renew(1, "", "u", "", later) == nil {
		t.Error("a published rating was renewed")
	}

	// The moderation removes, alters or releases.
	removed := published()
	removed.UnderReview = true
	if err := removed.Remove("mod-1", "ofensivo", later); err != nil || removed.Status != rating.StatusRemoved || removed.UnderReview ||
		removed.ModerationNote != "ofensivo" || removed.ModeratedBy != "mod-1" || removed.ModeratedAt == nil {
		t.Fatalf("removing: %v, %+v", err, removed)
	}
	if removed.Remove("mod-1", "de novo", later) == nil || removed.Alter("x", 0, "mod-1", "y", later) == nil || removed.Revise(1, "", later, 0) == nil {
		t.Error("a removed rating was removed again, altered or edited")
	}
	altered := published()
	altered.UnderReview = true
	if err := altered.Alter("limpo", 0, "mod-1", "retirado o insulto", later); err != nil || altered.Body != "limpo" || altered.Score != 4 ||
		altered.UnderReview || altered.Status != rating.StatusPublished || altered.ModerationNote == "" {
		t.Fatalf("altering: %v, %+v", err, altered)
	}
	if err := altered.Alter("limpo", 2, "mod-1", "nota", later); err != nil || altered.Score != 2 {
		t.Errorf("altering the stars: %v, %d", err, altered.Score)
	}
	if altered.Alter("x", 9, "mod-1", "nota", later) == nil {
		t.Error("a score that is not one was taken by an alteration")
	}
	released := published()
	released.UnderReview = true
	released.Release(later)
	if released.UnderReview || released.Body != "bom" || released.Status != rating.StatusPublished {
		t.Errorf("releasing changed the rating: %+v", released)
	}
}

func TestTheReportsOwnRules(t *testing.T) {
	t.Parallel()

	now := mustTime("2026-11-03T12:00:00Z")
	open := func() *rating.Report {
		return &rating.Report{ID: "p-1", Status: rating.ReportOpen, ScoreBefore: 1, BodyBefore: "ofensivo"}
	}

	r := open()
	if err := r.Uphold(rating.OutcomeAltered, "mod-1", "Rita", "retirado o insulto", 1, "limpo", now); err != nil ||
		r.Status != rating.ReportUpheld || r.Outcome != rating.OutcomeAltered || r.DecidedBy != "mod-1" || r.DecidedAt == nil ||
		r.BodyBefore != "ofensivo" || r.BodyAfter != "limpo" {
		t.Fatalf("upholding: %v, %+v", err, r)
	}
	if r.Uphold(rating.OutcomeRemoved, "mod-2", "Davi", "x", 0, "", now) == nil || r.Dismiss("mod-2", "Davi", "x", now) == nil {
		t.Error("a decided report took another decision")
	}
	if open().Uphold(rating.OutcomeNone, "m", "", "x", 0, "", now) == nil || open().Uphold("bogus", "m", "", "x", 0, "", now) == nil {
		t.Error("an upheld report without an outcome was taken")
	}
	d := open()
	if err := d.Dismiss("mod-1", "Rita", "sem motivo", now); err != nil || d.Status != rating.ReportDismissed || d.Outcome != rating.OutcomeNone ||
		d.BodyAfter != d.BodyBefore || d.ScoreAfter != d.ScoreBefore {
		t.Fatalf("dismissing: %v, %+v", err, d)
	}
}

func TestASummaryIsTheAverageOfWhatItCounts(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		summary rating.Summary
		rounded float64
		stars   int
	}{
		{rating.Summary{}, 0, 0},
		{rating.Summary{Count: 3, Average: 13.0 / 3}, 4.3, 4},
		{rating.Summary{Count: 2, Average: 4.5}, 4.5, 5},
		{rating.Summary{Count: 1, Average: 1}, 1, 1},
	} {
		if got := c.summary.Rounded(); got != c.rounded {
			t.Errorf("%+v rounds to %v, want %v", c.summary, got, c.rounded)
		}
		if got := c.summary.Stars(); got != c.stars {
			t.Errorf("%+v is %d stars, want %d", c.summary, got, c.stars)
		}
	}
}

func TestARefIsAcceptedOnlyWhenItCanBeStoredAndAddressed(t *testing.T) {
	t.Parallel()

	for ref, want := range map[rating.Ref]bool{
		{Type: "application", ID: "42"}:                    true,
		{Type: "plan.pro", ID: "A1_b-2"}:                   true,
		{Type: "application", ID: "a:b"}:                   true,
		{Type: "", ID: "42"}:                               false,
		{Type: "application", ID: ""}:                      false,
		{Type: "Application", ID: "42"}:                    false,
		{Type: "application", ID: "a/b"}:                   false,
		{Type: "application", ID: "a b"}:                   false,
		{Type: "application", ID: "../x"}:                  false,
		{Type: ".hidden", ID: "42"}:                        false,
		{Type: "application", ID: strings.Repeat("x", 65)}: false,
	} {
		if got := ref.Valid(); got != want {
			t.Errorf("%+v is valid = %v, want %v", ref, got, want)
		}
	}
}

func TestTheConfigurationRefusesWhatCannotWork(t *testing.T) {
	t.Parallel()

	with := func(change func(*rating.Config)) rating.Config {
		cfg := wired()
		change(&cfg)
		return cfg
	}
	for name, cfg := range map[string]rating.Config{
		"no team tenant":            with(func(c *rating.Config) { c.TeamTenant = "" }),
		"no subjects":               with(func(c *rating.Config) { c.Subjects = nil }),
		"no eligibility":            with(func(c *rating.Config) { c.Eligibility = nil }),
		"relative prefix":           with(func(c *rating.Config) { c.Prefix = "ratings" }),
		"framework namespace":       with(func(c *rating.Config) { c.OwnerPrefix = "/_arandu/ratings" }),
		"the same prefix twice":     with(func(c *rating.Config) { c.Prefix = "/x"; c.TeamPrefix = "/x" }),
		"page size too big":         with(func(c *rating.Config) { c.PageSize = rating.MaxPageSize + 1 }),
		"negative page size":        with(func(c *rating.Config) { c.PageSize = -1 }),
		"text too long":             with(func(c *rating.Config) { c.MaxBodyChars = rating.MaxBodyCharsLimit + 1 }),
		"negative reason":           with(func(c *rating.Config) { c.MaxReasonChars = -1 }),
		"note too long":             with(func(c *rating.Config) { c.MaxNoteChars = rating.MaxNoteCharsLimit + 1 }),
		"negative window":           with(func(c *rating.Config) { c.EditWindow = -time.Second }),
		"a segment with a wildcard": with(func(c *rating.Config) { c.Paths = rating.Paths{Edit: "/{x}"} }),
	} {
		if err := cfg.Validate(); err == nil {
			t.Errorf("the configuration with %s was accepted", name)
		}
	}

	// The configuration an application that changes nothing writes names the
	// team's tenant and the two things only it can say, and no tenant of a
	// customer's: every customer's tenant comes from the Grant.
	if err := wired().Validate(); err != nil {
		t.Fatalf("the smallest configuration was refused: %v", err)
	}
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
