package feature_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/arandu-io/framework/data"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/database/migrations"

	rating "github.com/hyz-is/arandu-rating"
)

// These tests drive the module the way an application does: build it, register
// its routes on a router, sign people in, and make requests. The screens are
// drawn from views the application compiles, so the pages themselves are held
// by tests/Unit; here every route answers the JSON a client asks for, and every
// form is posted the way a browser posts it.

// reservedPrefix is the namespace the framework keeps for itself: the health
// probe, the reload endpoint, the development console, the addressed assets.
const reservedPrefix = "/_arandu"

// site is the module registered on a router the way the kernel builds one.
type site struct {
	world
	router *fhttp.Router
	flash  *security.Flash
}

// serve registers the routes of a world's module.
func serve(t *testing.T, w world) site {
	t.Helper()
	flash := security.NewFlash([]byte(appKey), false)
	router := fhttp.NewRouter().WithFlash(flash)
	w.module.Routes(router.ForModule(w.module.Name()))
	return site{world: w, router: router, flash: flash}
}

// signIn starts a session for the subject and returns the cookies a browser
// would send back. Only the account is written to the session; the actions
// come back through the store's resolver, as an application's would.
func (s site) signIn(t *testing.T, subject security.Subject) []*http.Cookie {
	t.Helper()
	remember(subject)
	rec := httptest.NewRecorder()
	if _, err := s.sessions.Start(context.Background(), rec, security.Subject{ID: subject.ID, Tenant: subject.Tenant}); err != nil {
		t.Fatalf("starting a session: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("starting a session wrote no cookie")
	}
	return cookies
}

// request is one request to make.
type request struct {
	method  string
	target  string
	form    url.Values
	json    bool
	cookies []*http.Cookie
	body    *bytes.Buffer
	ctype   string
}

// answer makes one request and returns the recorder. A panic out of the router
// is a failure of the test: it is how an error nobody claimed arrives here.
func (s site) answer(t *testing.T, r request) (rec *httptest.ResponseRecorder) {
	t.Helper()
	var body *strings.Reader
	switch {
	case r.body != nil:
		body = strings.NewReader(r.body.String())
	case r.form != nil:
		body = strings.NewReader(r.form.Encode())
	default:
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(r.method, r.target, body)
	switch {
	case r.ctype != "":
		req.Header.Set("Content-Type", r.ctype)
	case r.form != nil:
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if r.json {
		req.Header.Set("Accept", "application/json")
	}
	for _, cookie := range r.cookies {
		req.AddCookie(cookie)
	}
	rec = httptest.NewRecorder()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s %s panicked, which a deployed router answers with 500: %v", r.method, r.target, recovered)
		}
	}()
	s.router.ServeHTTP(rec, req)
	return rec
}

// decode reads a JSON response into a map.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response is not JSON (%d): %s", rec.Code, rec.Body.String())
	}
	if data, ok := out["data"].(map[string]any); ok {
		return data
	}
	return out
}

// everyRoute is a request to each route the module registers, under the
// default prefixes. TestTheModuleRegistersItsRoutesUnderItsPrefixes fails when
// a route exists that this list does not reach.
var everyRoute = []struct {
	method  string
	target  string
	pattern string
	side    string
}{
	{http.MethodGet, "/ratings", "/ratings", "author"},
	{http.MethodGet, "/ratings/subject/application/42", "/ratings/subject/{type}/{id}", "author"},
	{http.MethodPost, "/ratings/subject/application/42", "/ratings/subject/{type}/{id}", "author"},
	{http.MethodGet, "/ratings/r-1", "/ratings/{id}", "author"},
	{http.MethodPost, "/ratings/r-1/edit", "/ratings/{id}/edit", "author"},
	{http.MethodPost, "/ratings/r-1/withdrawal", "/ratings/{id}/withdrawal", "author"},

	{http.MethodGet, "/ratings/received", "/ratings/received", "owner"},
	{http.MethodGet, "/ratings/received/subject/application/42", "/ratings/received/subject/{type}/{id}", "owner"},
	{http.MethodGet, "/ratings/received/reports", "/ratings/received/reports", "owner"},
	{http.MethodGet, "/ratings/received/reports/r-1", "/ratings/received/reports/{rating}", "owner"},
	{http.MethodPost, "/ratings/received/reports/r-1", "/ratings/received/reports/{rating}", "owner"},

	{http.MethodGet, "/admin/ratings", "/admin/ratings", "team"},
	{http.MethodGet, "/admin/ratings/p-1", "/admin/ratings/{id}", "team"},
	{http.MethodPost, "/admin/ratings/p-1/decision", "/admin/ratings/{id}/decision", "team"},
}

func TestAVisitorWithNoSessionIsSentToSignIn(t *testing.T) {
	t.Parallel()
	s := serve(t, install(t))

	// RequireAuth answers before the handler runs, so a visitor with no
	// session reaches neither the policy nor the database.
	for _, r := range everyRoute {
		rec := s.answer(t, request{method: r.method, target: r.target})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != middleware.SignInPath {
			t.Errorf("%s %s answered %d to %q, want %d to %s", r.method, r.target, rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, middleware.SignInPath)
		}
	}
}

func TestEachSideRefusesTheSubjectsOfTheOthers(t *testing.T) {
	t.Parallel()
	// A form that passes every check, so what refuses is the policy and not the
	// validation that comes first.
	validForm := url.Values{"score": {"4"}, "body": {"texto"}, "reason": {"motivo"}, "verdict": {"dismiss"}, "note": {"nota"}}
	s := serve(t, install(t))
	people := map[string][]*http.Cookie{
		"author": s.signIn(t, author("acme", "user-ana")),
		"owner":  s.signIn(t, owner("globex", "user-gil")),
		"team":   s.signIn(t, moderator("mod-1")),
	}

	for _, r := range everyRoute {
		for who, cookies := range people {
			if who == r.side {
				continue
			}
			rec := s.answer(t, request{method: r.method, target: r.target, cookies: cookies, json: true, form: validForm})
			if rec.Code != http.StatusForbidden {
				t.Errorf("the %s on the %s's %s %s was answered %d, want 403", who, r.side, r.method, r.target, rec.Code)
			}
		}
	}
}

func TestAnAuthorRatesReadsChangesAndDeletesOverHTTP(t *testing.T) {
	t.Parallel()
	s := serve(t, install(t))
	ana := s.signIn(t, author("acme", "user-ana"))
	subjectPath := "/ratings/subject/application/42"

	// The form for a subject says what it is, and that nothing is rated yet.
	rec := s.answer(t, request{method: http.MethodGet, target: subjectPath, cookies: ana, json: true})
	card := decode(t, rec)
	if rec.Code != http.StatusOK || card["rated"] != false || card["subject"].(map[string]any)["name"] != "Câmera Norte" {
		t.Fatalf("the form answered %d: %s", rec.Code, rec.Body)
	}
	if _, leaks := card["subject"].(map[string]any)["owner_tenant"]; leaks {
		t.Fatal("the subject names the tenant that owns it")
	}

	// Rating redirects to the rating.
	rec = s.answer(t, request{method: http.MethodPost, target: subjectPath, cookies: ana, form: url.Values{"score": {"4"}, "body": {"Boa câmera."}}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/ratings/") {
		t.Fatalf("rating answered %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	mine := rec.Header().Get("Location")

	// Asking for the form again leads to the rating, for a browser and for JSON.
	rec = s.answer(t, request{method: http.MethodGet, target: subjectPath, cookies: ana})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != mine {
		t.Fatalf("the form of a subject already rated answered %d to %q, want %s", rec.Code, rec.Header().Get("Location"), mine)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: subjectPath, cookies: ana, json: true})
	if again := decode(t, rec); again["rated"] != true || "/ratings/"+again["rating_id"].(string) != mine {
		t.Fatalf("the form for JSON answered %s", rec.Body)
	}

	rec = s.answer(t, request{method: http.MethodGet, target: mine, cookies: ana, json: true})
	shown := decode(t, rec)
	if rec.Code != http.StatusOK || shown["score"] != float64(4) || shown["body"] != "Boa câmera." || shown["status"] != "published" || shown["under_review"] != false ||
		shown["subject"].(map[string]any)["count"] != float64(1) {
		t.Fatalf("reading answered %d: %s", rec.Code, rec.Body)
	}
	// The author's answer carries nothing of the account's tenant or of whoever reported.
	for _, internal := range []string{"tenant", "tenant_id", "author_id", "author_name", "reporter_id"} {
		if _, ok := shown[internal]; ok {
			t.Errorf("the author's rating carries %q", internal)
		}
	}

	rec = s.answer(t, request{method: http.MethodPost, target: mine + "/edit", cookies: ana, json: true, form: url.Values{"score": {"5"}, "body": {"Ótima."}}})
	if rec.Code != http.StatusOK || decode(t, rec)["score"] != float64(5) || decode(t, rec)["edited_at"] == nil {
		t.Fatalf("changing answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: mine + "/edit", cookies: ana, form: url.Values{"score": {"3"}}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != mine {
		t.Fatalf("changing for a browser answered %d to %q", rec.Code, rec.Header().Get("Location"))
	}

	// The list, searched and ordered; an address somebody edited by hand is not
	// a form to send back.
	rec = s.answer(t, request{method: http.MethodGet, target: "/ratings?q=boa&status=published&score=3&sort=score&dir=asc", cookies: ana, json: true})
	list := decode(t, rec)
	if rec.Code != http.StatusOK || list["total"] != float64(0) {
		t.Fatalf("listing answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: "/ratings?sort=tenant_id&status=bogus&score=9&page=-4", cookies: ana, json: true})
	if rec.Code != http.StatusOK || decode(t, rec)["total"] != float64(1) {
		t.Fatalf("a hand-edited address answered %d: %s", rec.Code, rec.Body)
	}

	// Deleting answers nothing, and the rating is gone.
	rec = s.answer(t, request{method: http.MethodPost, target: mine + "/withdrawal", cookies: ana, json: true})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("deleting answered %d: %s", rec.Code, rec.Body)
	}
	if rec := s.answer(t, request{method: http.MethodGet, target: mine, cookies: ana, json: true}); rec.Code != http.StatusNotFound {
		t.Fatalf("a deleted rating answered %d", rec.Code)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: subjectPath + "x", cookies: ana, json: true, form: url.Values{"score": {"3"}}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a subject nobody knows answered %d", rec.Code)
	}
}

func TestARejectedFormGoesBackWithTheMessagesInPortuguese(t *testing.T) {
	t.Parallel()
	s := serve(t, install(t))
	ana := s.signIn(t, author("acme", "user-ana"))
	target := "/ratings/subject/application/42"

	rec := s.answer(t, request{method: http.MethodPost, target: target, cookies: ana, form: url.Values{"score": {""}, "body": {"sem nota"}}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("a form with no stars answered %d to %q, want %d back where it came from", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	next := httptest.NewRequest(http.MethodGet, "/", nil)
	next.Header.Set("Accept", "text/html")
	for _, cookie := range rec.Result().Cookies() {
		next.AddCookie(cookie)
	}
	errs, old, ok := s.flash.Take(httptest.NewRecorder(), next)
	if !ok || len(errs["score"]) == 0 || errs["score"][0] != "Escolha de uma a cinco estrelas." {
		t.Fatalf("the redirect carries %v in the flash", errs)
	}
	// What was typed comes back with the messages, so the form is not emptied.
	if len(old["body"]) != 1 || old["body"][0] != "sem nota" {
		t.Fatalf("the redirect carries %v of what was typed", old)
	}

	rec = s.answer(t, request{method: http.MethodPost, target: target, cookies: ana, json: true, form: url.Values{"score": {"7"}}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Escolha de uma a cinco estrelas.") {
		t.Fatalf("a form with seven stars for JSON answered %d: %s", rec.Code, rec.Body)
	}
}

func TestARatingOfAnotherSpaceIsNotFoundOverHTTP(t *testing.T) {
	t.Parallel()
	w := install(t)
	s := serve(t, w)
	theirs := rate(t, w, author("acme", "user-ana"), camera, 3, "da Acme")
	hooli := s.signIn(t, manager("hooli", "user-boss"))

	for _, r := range []request{
		{method: http.MethodGet, target: "/ratings/" + theirs.ID},
		{method: http.MethodPost, target: "/ratings/" + theirs.ID + "/edit", form: url.Values{"score": {"1"}}},
		{method: http.MethodPost, target: "/ratings/" + theirs.ID + "/withdrawal"},
		{method: http.MethodGet, target: "/ratings/no-such-rating"},
	} {
		r.cookies, r.json = hooli, true
		if rec := s.answer(t, r); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s answered %d, want 404", r.method, r.target, rec.Code)
		}
	}
	// Nothing the request carries chooses the space: a tenant in the path, the
	// query or the body is a value the caller chose, and is read by nobody.
	rec := s.answer(t, request{method: http.MethodPost, target: "/ratings/subject/application/42?tenant=acme&tenant_id=acme", cookies: hooli, json: true,
		form: url.Values{"score": {"2"}, "body": {"Tentando"}, "tenant": {"acme"}, "tenant_id": {"acme"}, "author_id": {"user-ana"}}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("rating answered %d: %s", rec.Code, rec.Body)
	}
	var tenant, authorID string
	if err := w.sql.QueryRow(`SELECT tenant_id, author_id FROM ratings WHERE body = 'Tentando'`).Scan(&tenant, &authorID); err != nil || tenant != "hooli" || authorID != "user-boss" {
		t.Fatalf("the rating is in %q by %q, %v; the tenant and the author are the session's", tenant, authorID, err)
	}
}

func TestTheOwnerReadsAndReportsOverHTTP(t *testing.T) {
	t.Parallel()
	w := install(t)
	s := serve(t, w)
	rated := rate(t, w, author("acme", "user-ana"), camera, 2, "tela preta, <b>não funciona</b>")
	gil := s.signIn(t, owner("globex", "user-gil"))

	rec := s.answer(t, request{method: http.MethodGet, target: "/ratings/received", cookies: gil, json: true})
	list := decode(t, rec)
	if rec.Code != http.StatusOK || list["total"] != float64(2) {
		t.Fatalf("the subjects answered %d: %s", rec.Code, rec.Body)
	}
	camara := list["items"].([]any)[1].(map[string]any)
	if camara["name"] != "Câmera Norte" || camara["count"] != float64(1) || camara["average"] != float64(2) {
		t.Fatalf("the subject is %v", camara)
	}

	rec = s.answer(t, request{method: http.MethodGet, target: "/ratings/received/subject/application/42?sort=score", cookies: gil, json: true})
	received := decode(t, rec)
	items := received["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 1 {
		t.Fatalf("the ratings answered %d: %s", rec.Code, rec.Body)
	}
	first := items[0].(map[string]any)
	if first["author_name"] != "Ana Souza" || first["body"] != "tela preta, <b>não funciona</b>" {
		t.Fatalf("the rating is %v", first)
	}
	// The owner reads what was said and by whom by name, and not which account.
	for _, hidden := range []string{"tenant", "tenant_id", "author_id", "status", "moderation_note"} {
		if _, ok := first[hidden]; ok {
			t.Errorf("the owner's rating carries %q", hidden)
		}
	}

	// The form for the report is the rating; the post files it.
	rec = s.answer(t, request{method: http.MethodGet, target: "/ratings/received/reports/" + rated.ID, cookies: gil, json: true})
	if rec.Code != http.StatusOK || decode(t, rec)["id"] != rated.ID {
		t.Fatalf("the report form answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: "/ratings/received/reports/" + rated.ID, cookies: gil, json: true, form: url.Values{"reason": {"Nunca usou."}}})
	filed := decode(t, rec)
	if rec.Code != http.StatusCreated || filed["status"] != "open" || filed["reason"] != "Nunca usou." {
		t.Fatalf("reporting answered %d: %s", rec.Code, rec.Body)
	}
	for _, hidden := range []string{"tenant", "rating_tenant", "reporter_id", "decided_by", "body_before"} {
		if _, ok := filed[hidden]; ok {
			t.Errorf("the owner's report carries %q", hidden)
		}
	}
	// Reported again, for a browser: refused, and sent back with the sentence.
	rec = s.answer(t, request{method: http.MethodPost, target: "/ratings/received/reports/" + rated.ID, cookies: gil, form: url.Values{"reason": {"Outra vez."}}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a second report for a browser answered %d", rec.Code)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: "/ratings/received/reports?status=open", cookies: gil, json: true})
	if rec.Code != http.StatusOK || decode(t, rec)["total"] != float64(1) {
		t.Fatalf("the reports answered %d: %s", rec.Code, rec.Body)
	}

	// Nobody else reads these ratings or reports this one.
	other := s.signIn(t, owner("initech", "user-i"))
	for _, r := range []request{
		{method: http.MethodGet, target: "/ratings/received/subject/application/42"},
		{method: http.MethodGet, target: "/ratings/received/reports/" + rated.ID},
		{method: http.MethodPost, target: "/ratings/received/reports/" + rated.ID, form: url.Values{"reason": {"x"}}},
	} {
		r.cookies, r.json = other, true
		if rec := s.answer(t, r); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s by another owner answered %d, want 404", r.method, r.target, rec.Code)
		}
	}
}

func TestTheModerationDecidesOverHTTP(t *testing.T) {
	t.Parallel()
	w := install(t)
	s := serve(t, w)
	rated := rate(t, w, author("acme", "user-ana"), camera, 1, "ofensivo")
	filed := report(t, w, owner("globex", "user-gil"), rated.ID, "ofende")
	rita := s.signIn(t, moderator("mod-1"))
	base := "/admin/ratings/" + filed.ID

	// The queue opens on the work waiting.
	rec := s.answer(t, request{method: http.MethodGet, target: "/admin/ratings", cookies: rita, json: true})
	queue := decode(t, rec)
	if rec.Code != http.StatusOK || queue["total"] != float64(1) || queue["items"].([]any)[0].(map[string]any)["tenant"] != "globex" ||
		queue["items"].([]any)[0].(map[string]any)["rating_tenant"] != "acme" {
		t.Fatalf("the queue answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: base, cookies: rita, json: true})
	shown := decode(t, rec)
	if rec.Code != http.StatusOK || shown["rating"].(map[string]any)["body"] != "ofensivo" || shown["rating"].(map[string]any)["tenant"] != "acme" ||
		shown["report"].(map[string]any)["reason"] != "ofende" {
		t.Fatalf("the report answered %d: %s", rec.Code, rec.Body)
	}

	// A decision with no justification goes back with the sentence.
	rec = s.answer(t, request{method: http.MethodPost, target: base + "/decision", cookies: rita, json: true, form: url.Values{"verdict": {"remove"}}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Preencha este campo.") {
		t.Fatalf("a decision with no justification answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: base + "/decision", cookies: rita, json: true, form: url.Values{"verdict": {"remove"}, "note": {"Ofensa."}}})
	decided := decode(t, rec)
	if rec.Code != http.StatusOK || decided["status"] != "upheld" || decided["outcome"] != "removed" || decided["decided_by"] != "mod-1" || decided["decided_by_name"] != "Rita" ||
		decided["decision_note"] != "Ofensa." || decided["decided_at"] == nil {
		t.Fatalf("deciding answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: base + "/decision", cookies: rita, form: url.Values{"verdict": {"dismiss"}, "note": {"de novo"}}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a second decision for a browser answered %d", rec.Code)
	}

	// What waits is now nothing; the decided are one request away.
	rec = s.answer(t, request{method: http.MethodGet, target: "/admin/ratings", cookies: rita, json: true})
	if decode(t, rec)["total"] != float64(0) {
		t.Fatalf("the queue still holds %s", rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: "/admin/ratings?status=upheld&sort=created_at&dir=desc", cookies: rita, json: true})
	if decode(t, rec)["total"] != float64(1) {
		t.Fatalf("the decided reports answered %s", rec.Body)
	}
	if rec := s.answer(t, request{method: http.MethodGet, target: "/admin/ratings/no-such-report", cookies: rita, json: true}); rec.Code != http.StatusNotFound {
		t.Fatalf("a report that does not exist answered %d", rec.Code)
	}
	// The author reads the reason, and the owner follows the outcome.
	ana := s.signIn(t, author("acme", "user-ana"))
	rec = s.answer(t, request{method: http.MethodGet, target: "/ratings/" + rated.ID, cookies: ana, json: true})
	if got := decode(t, rec); got["status"] != "removed" || got["moderation_note"] != "Ofensa." {
		t.Fatalf("the author reads %s", rec.Body)
	}
}

func TestTheModuleRegistersItsRoutesUnderItsPrefixes(t *testing.T) {
	t.Parallel()
	s := serve(t, install(t))

	got := make([]string, 0, len(everyRoute))
	for _, route := range s.router.Routes() {
		if route.Module != "rating" {
			t.Errorf("the route %s %s is not tagged with the module name: %q", route.Method, route.Pattern, route.Module)
		}
		got = append(got, route.Method+" "+route.Pattern)
	}
	want := make([]string, 0, len(everyRoute))
	for _, r := range everyRoute {
		want = append(want, r.method+" "+r.pattern)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("registered\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// The names are what a URL is built from.
	names := map[string]bool{}
	for _, route := range s.router.Routes() {
		names[route.RouteName()] = true
	}
	for _, name := range []string{"rating.index", "rating.show", "rating.owner.report", "rating.team.index", "rating.team.decide"} {
		if !names[name] {
			t.Errorf("no route is named %s", name)
		}
	}
}

func TestThePrefixesAndTheSegmentsMoveTheRoutes(t *testing.T) {
	t.Parallel()
	w := installWith(t, func(c *rating.Config) {
		c.Prefix, c.OwnerPrefix, c.TeamPrefix, c.Paths = "/painel/avaliacoes", "/painel/avaliacoes-recebidas", "/admin/avaliacoes", rating.PortuguesePaths
	})
	s := serve(t, w)
	ana := s.signIn(t, author("acme", "user-ana"))

	rec := s.answer(t, request{method: http.MethodPost, target: "/painel/avaliacoes/assunto/application/42", cookies: ana, json: true, form: url.Values{"score": {"5"}}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("rating under the new prefix answered %d: %s", rec.Code, rec.Body)
	}
	id := decode(t, rec)["id"].(string)
	if rec := s.answer(t, request{method: http.MethodPost, target: "/painel/avaliacoes/" + id + "/edicao", cookies: ana, json: true, form: url.Values{"score": {"4"}}}); rec.Code != http.StatusOK {
		t.Fatalf("changing under the new segments answered %d: %s", rec.Code, rec.Body)
	}
	gil := s.signIn(t, owner("globex", "user-gil"))
	if rec := s.answer(t, request{method: http.MethodPost, target: "/painel/avaliacoes-recebidas/denuncias/" + id, cookies: gil, json: true, form: url.Values{"reason": {"x"}}}); rec.Code != http.StatusCreated {
		t.Fatalf("reporting under the new segments answered %d: %s", rec.Code, rec.Body)
	}
	rita := s.signIn(t, moderator("mod-1"))
	var reportID string
	if err := w.sql.QueryRow(`SELECT id FROM rating_reports`).Scan(&reportID); err != nil {
		t.Fatal(err)
	}
	if rec := s.answer(t, request{method: http.MethodPost, target: "/admin/avaliacoes/" + reportID + "/decisao", cookies: rita, json: true, form: url.Values{"verdict": {"dismiss"}, "note": {"ok"}}}); rec.Code != http.StatusOK {
		t.Fatalf("deciding under the new segments answered %d: %s", rec.Code, rec.Body)
	}
	// What was at the old addresses is gone.
	if rec := s.answer(t, request{method: http.MethodGet, target: "/ratings", cookies: ana, json: true}); rec.Code != http.StatusNotFound {
		t.Fatalf("the old address answered %d, want 404", rec.Code)
	}

	// The URLs the screens link to follow the configuration.
	urls := w.module.URLs()
	if got := urls.Rate(rating.Ref{Type: "application", ID: "42"}); got != "/painel/avaliacoes/assunto/application/42" {
		t.Fatalf("the address to rate is %q", got)
	}
	if got := urls.OwnerReport("a b"); got != "/painel/avaliacoes-recebidas/denuncias/a%20b" {
		t.Fatalf("the report address is %q", got)
	}
	if got := urls.TeamDecide("abc"); got != "/admin/avaliacoes/abc/decisao" {
		t.Fatalf("the decision address is %q", got)
	}
}

func TestTheSidesAreMountedApartAndGuardedApart(t *testing.T) {
	t.Parallel()
	denied := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	}
	w := installWith(t, func(c *rating.Config) { c.TeamMiddleware = []func(http.Handler) http.Handler{denied} })

	// Mounted alone, each side registers its own routes and no others.
	customerOnly := fhttp.NewRouter()
	w.module.CustomerRoutes(customerOnly.ForModule("rating"))
	teamOnly := fhttp.NewRouter()
	w.module.TeamRoutes(teamOnly.ForModule("rating"))
	for _, route := range customerOnly.Routes() {
		if strings.HasPrefix(route.Pattern, "/admin") {
			t.Errorf("the customer's routes include %s", route.Pattern)
		}
	}
	for _, route := range teamOnly.Routes() {
		if !strings.HasPrefix(route.Pattern, "/admin") {
			t.Errorf("the team's routes include %s", route.Pattern)
		}
	}
	if len(customerOnly.Routes()) != 11 || len(teamOnly.Routes()) != 3 {
		t.Fatalf("the customer's side registers %d routes and the team's %d; want 11 and 3", len(customerOnly.Routes()), len(teamOnly.Routes()))
	}

	// A guard of the application's own side runs on that side alone.
	s := serve(t, w)
	rita := s.signIn(t, moderator("mod-1"))
	ana := s.signIn(t, author("acme", "user-ana"))
	if rec := s.answer(t, request{method: http.MethodGet, target: "/admin/ratings", cookies: rita, json: true}); rec.Code != http.StatusTeapot {
		t.Fatalf("the team's guard did not run: %d", rec.Code)
	}
	if rec := s.answer(t, request{method: http.MethodGet, target: "/ratings", cookies: ana, json: true}); rec.Code != http.StatusOK {
		t.Fatalf("the team's guard ran on the customer's side: %d", rec.Code)
	}
}

// TestNoRouteLandsInTheFrameworkNamespace is the one property of this package
// that syntax cannot hold: a prefix arrives through configuration, so the only
// way to know where the routes ended up is to register them and read the table
// back.
func TestNoRouteLandsInTheFrameworkNamespace(t *testing.T) {
	t.Parallel()

	for _, change := range []func(*rating.Config){
		func(*rating.Config) {},
		func(c *rating.Config) {
			c.Prefix, c.OwnerPrefix, c.TeamPrefix = "/painel/avaliacoes", "/painel/recebidas", "/admin/avaliacoes"
		},
	} {
		s := serve(t, installWith(t, change))
		registered := 0
		for _, route := range s.router.Routes() {
			registered++
			if route.Pattern == reservedPrefix || strings.HasPrefix(route.Pattern, reservedPrefix+"/") {
				t.Errorf("the route %s %s is registered under %s/, which the framework keeps for itself and refuses at boot", route.Method, route.Pattern, reservedPrefix)
			}
		}
		if registered == 0 {
			t.Fatal("the module registered no route, so this test proved nothing")
		}
	}
}

func TestNewRefusesAWiringThatCannotWork(t *testing.T) {
	t.Parallel()

	sessions := security.NewSessionStore([]byte(appKey), 3600e9, false, security.NewMemoryBackend())
	handle := data.Wrap(nil, data.DialectSQLite)
	valid := rating.Config{TeamTenant: realm, Subjects: theCatalogue(), Eligibility: &eligibility{}}
	with := func(change func(*rating.Config)) rating.Config {
		cfg := valid
		change(&cfg)
		return cfg
	}

	for name, cfg := range map[string]rating.Config{
		"no team tenant":            with(func(c *rating.Config) { c.TeamTenant = "" }),
		"a team tenant that is not": with(func(c *rating.Config) { c.TeamTenant = "Not/Valid" }),
		"no subjects":               with(func(c *rating.Config) { c.Subjects = nil }),
		"no eligibility":            with(func(c *rating.Config) { c.Eligibility = nil }),
		"a relative prefix":         with(func(c *rating.Config) { c.Prefix = "ratings" }),
		"a prefix in the framework": with(func(c *rating.Config) { c.TeamPrefix = "/_arandu/ratings" }),
		"the same prefix twice":     with(func(c *rating.Config) { c.Prefix, c.TeamPrefix = "/x", "/x" }),
		"a wildcard prefix":         with(func(c *rating.Config) { c.Prefix = "/ratings/{id}" }),
		"a segment with a wildcard": with(func(c *rating.Config) { c.Paths = rating.Paths{Edit: "/{edit}"} }),
		"a negative page size":      with(func(c *rating.Config) { c.PageSize = -1 }),
		"a page size too big":       with(func(c *rating.Config) { c.PageSize = rating.MaxPageSize + 1 }),
		"a text too long":           with(func(c *rating.Config) { c.MaxBodyChars = rating.MaxBodyCharsLimit + 1 }),
		"a nil guard":               with(func(c *rating.Config) { c.TeamMiddleware = []func(http.Handler) http.Handler{nil} }),
	} {
		if _, err := rating.New(cfg, handle, sessions); err == nil {
			t.Errorf("a configuration with %s was accepted", name)
		}
	}
	if _, err := rating.New(valid, nil, sessions); err == nil {
		t.Error("a nil database handle was accepted")
	}
	if _, err := rating.New(valid, handle, nil); err == nil {
		t.Error("a nil session store was accepted")
	}
	if _, err := rating.New(valid, handle, sessions); err != nil {
		t.Fatalf("a valid wiring was refused: %v", err)
	}
}

func TestTheModuleDeclaresItsSchema(t *testing.T) {
	t.Parallel()
	w := install(t)

	declared := w.module.Migrations()
	if len(declared) == 0 {
		t.Fatal("the module declares migrations = true and returns none")
	}
	names := make([]string, 0, len(declared))
	for _, migration := range declared {
		name := migration.GetName()
		if name == "" {
			t.Fatal("a migration has no name, and the name is what carries the order")
		}
		names = append(names, name)
		// A migration that cannot be rolled back is a deploy that cannot be
		// undone. The migrator finds Down by type assertion, so a Down with the
		// wrong signature is a rollback that silently does nothing.
		if _, ok := migration.(migrations.ReversibleMigration); !ok {
			t.Errorf("the migration %s has no Down", name)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("the migrations are not returned in the order their names sort in: %v", names)
	}

	// Down takes it all back, and Up can run again.
	conn := hedatabaseFor(w)
	for _, migration := range declared {
		if err := migration.(migrations.ReversibleMigration).Down(context.Background(), conn); err != nil {
			t.Fatalf("rolling back %s: %v", migration.GetName(), err)
		}
	}
	var left int
	if err := w.sql.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE 'rating%'`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d tables are left after the rollback, %v", left, err)
	}
	for _, migration := range declared {
		if err := migration.Up(context.Background(), conn); err != nil {
			t.Fatalf("applying %s again: %v", migration.GetName(), err)
		}
	}
}
