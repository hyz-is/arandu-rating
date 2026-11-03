package feature_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"

	rating "github.com/hyz-is/arandu-rating"
)

// count is how many rows a statement counts.
func count(t *testing.T, w world, statement string, args ...any) int {
	t.Helper()
	var n int
	if err := w.sql.QueryRow(statement, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	return n
}

// rejected is the validation errors an error carries, and fails the test when it
// carries none.
func rejected(t *testing.T, err error, what string) validation.Errors {
	t.Helper()
	var errs validation.Errors
	if !errors.As(err, &errs) {
		t.Fatalf("%s: got %v, want the form rejected", what, err)
	}
	return errs
}

func TestAnAuthorRatesEditsAndWithdraws(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := author("acme", "user-ana")

	created := rate(t, w, ana, camera, 4, "  Bom, mas trava.  ")
	// The tenant is the Grant's, the author is the session's, the text is as
	// the author wrote it without the space around it, and it is published.
	if created.TenantID != "acme" || created.AuthorID != "user-ana" || created.AuthorName != "Ana Souza" ||
		created.Score != 4 || created.Body != "Bom, mas trava." || created.Status != rating.StatusPublished ||
		bool(created.UnderReview) || created.EditedAt != nil || created.SubjectType != "application" || created.SubjectID != "42" {
		t.Fatalf("the rating is %+v", created)
	}

	// She changes her mind.
	w.clock.advance(time.Hour)
	changed, err := w.ratings.Update(ctx, ana, created.ID, rating.RateRequest{Score: 5, Body: "Ótima depois da atualização."})
	if err != nil || changed.Score != 5 || changed.EditedAt == nil || !changed.EditedAt.After(created.CreatedAt) {
		t.Fatalf("updating: %v, %+v", err, changed)
	}
	detail, err := w.ratings.Find(ctx, ana, created.ID)
	if err != nil || detail.Rating.Score != 5 || detail.Rating.Body != "Ótima depois da atualização." || !detail.CanEdit || !detail.CanWithdraw ||
		detail.Subject.Name != "Câmera Norte" || detail.Summary.Count != 1 {
		t.Fatalf("finding: %v, %+v", err, detail)
	}

	// And then deletes it: the text is gone, the row stays, nothing reads it.
	if err := w.ratings.Withdraw(ctx, ana, created.ID); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}
	if _, err := w.ratings.Find(ctx, ana, created.ID); !errors.Is(err, rating.ErrNotFound) {
		t.Fatalf("a withdrawn rating is read: %v", err)
	}
	var status, body string
	if err := w.sql.QueryRow(`SELECT status, body FROM ratings WHERE id = ?`, created.ID).Scan(&status, &body); err != nil || status != "withdrawn" || body != "" {
		t.Fatalf("the row is %q %q, %v; want withdrawn with no text", status, body, err)
	}
	if _, err := w.ratings.Update(ctx, ana, created.ID, rating.RateRequest{Score: 1}); !errors.Is(err, rating.ErrNotFound) {
		t.Fatalf("a withdrawn rating takes a change: %v", err)
	}

	// She can rate it again, and it is the same row: an account rates a
	// subject once.
	again, err := w.ratings.Create(ctx, ana, camera, rating.RateRequest{Score: 2, Body: "Voltei atrás."})
	if err != nil || again.ID != created.ID || again.Status != rating.StatusPublished || again.Score != 2 || again.EditedAt != nil {
		t.Fatalf("rating again: %v, %+v", err, again)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM ratings`); n != 1 {
		t.Fatalf("%d rows after rating, withdrawing and rating again; want one", n)
	}
}

func TestAnAccountRatesASubjectOnce(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()

	first := rate(t, w, author("acme", "user-ana"), camera, 5, "")
	// Another person of the same space is the same account.
	_, err := w.ratings.Create(ctx, author("acme", "user-bia"), camera, rating.RateRequest{Score: 1, Body: "Eu discordo."})
	errs := rejected(t, err, "a second rating of the account")
	if got := errs.Get("score"); len(got) != 1 || !strings.Contains(got[0], "já avaliou") {
		t.Fatalf("the refusal is %v", errs)
	}
	// Another account rates it too, and so does the same one a different subject.
	rate(t, w, author("hooli", "user-hu"), camera, 3, "")
	rate(t, w, author("acme", "user-ana"), alarm, 2, "")
	if n := count(t, w, `SELECT COUNT(*) FROM ratings`); n != 3 {
		t.Fatalf("%d rows; want one per account and subject", n)
	}
	if first.Score != 5 {
		t.Fatal("the first rating was overwritten")
	}

	// The database holds the same line when the check is raced.
	if _, err := w.sql.Exec(`INSERT INTO ratings (id, tenant_id, subject_type, subject_id, author_id, author_name, score, body, status, under_review,
		moderation_note, moderated_by, created_at, updated_at) VALUES ('x', 'acme', 'application', '42', 'u', '', 3, '', 'published', 0, '', '', ?, ?)`,
		time.Now(), time.Now()); err == nil {
		t.Fatal("the database took a second rating of the same account and subject")
	}
}

func TestTheApplicationDecidesWhoMayRateWhat(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := author("acme", "user-ana")
	stars := rating.RateRequest{Score: 4}

	w.eligible.deny("acme", "user-ana", camera)
	_, err := w.ratings.Create(ctx, ana, camera, stars)
	requireErr(t, err, security.ErrForbidden, "rating what the application says the account may not")
	requireErr(t, w.ratings.CanRate(ctx, ana, camera), security.ErrForbidden, "asking whether the account may")
	_, err = w.ratings.Card(ctx, ana, camera)
	requireErr(t, err, security.ErrForbidden, "drawing the form the account may not submit")
	if err := w.ratings.CanRate(ctx, ana, alarm); err != nil {
		t.Fatalf("a subject the account may rate was refused: %v", err)
	}

	// The owner does not rate what it owns, whatever Eligibility says.
	_, err = w.ratings.Create(ctx, author("globex", "user-gil"), camera, stars)
	requireErr(t, err, security.ErrForbidden, "rating what the account owns")

	// A subject that does not exist, or that cannot be one, is not found.
	_, err = w.ratings.Create(ctx, ana, ghost, stars)
	requireErr(t, err, rating.ErrNotFound, "rating a subject nobody knows")
	_, err = w.ratings.Create(ctx, ana, rating.Ref{Type: "Bad Type", ID: "../x"}, stars)
	requireErr(t, err, rating.ErrNotFound, "rating a reference that cannot be one")

	if n := count(t, w, `SELECT COUNT(*) FROM ratings`); n != 0 {
		t.Fatalf("%d rows were written by refusals", n)
	}
	// And the application is asked about the account and the user the session
	// names, never about a tenant a request carried.
	for _, asked := range w.eligible.asked {
		if !strings.HasPrefix(asked, "acme/user-ana/") && !strings.HasPrefix(asked, "globex/user-gil/") {
			t.Errorf("Eligibility was asked about %q", asked)
		}
	}
}

func TestARatingIsValidatedBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := author("acme", "user-ana")

	for name, c := range map[string]struct {
		in    rating.RateRequest
		field string
	}{
		"no stars":        {rating.RateRequest{Score: 0, Body: "x"}, "score"},
		"too many stars":  {rating.RateRequest{Score: 6, Body: "x"}, "score"},
		"negative stars":  {rating.RateRequest{Score: -1}, "score"},
		"a text too long": {rating.RateRequest{Score: 3, Body: strings.Repeat("a", rating.DefaultMaxBodyChars+1)}, "body"},
	} {
		_, err := w.ratings.Create(ctx, ana, camera, c.in)
		if errs := rejected(t, err, name); len(errs.Get(c.field)) == 0 {
			t.Errorf("%s: the refusal is %v, want it on %s", name, errs, c.field)
		}
	}
	// The text counts characters, not bytes.
	if _, err := w.ratings.Create(ctx, ana, camera, rating.RateRequest{Score: 3, Body: strings.Repeat("ã", rating.DefaultMaxBodyChars)}); err != nil {
		t.Fatalf("a text of exactly the limit, in characters, was refused: %v", err)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM ratings`); n != 1 {
		t.Fatalf("%d rows; the refusals wrote some", n)
	}
}

func TestTheLimitsAreTheApplicationsToSet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	short := installWith(t, func(c *rating.Config) { c.MaxBodyChars = 10; c.RequireBody = true })
	ana := author("acme", "user-ana")
	_, err := short.ratings.Create(ctx, ana, camera, rating.RateRequest{Score: 4, Body: "onze letras"})
	rejected(t, err, "a text past the configured limit")
	_, err = short.ratings.Create(ctx, ana, camera, rating.RateRequest{Score: 4})
	rejected(t, err, "stars alone where the text is required")
	rate(t, short, ana, camera, 4, "dez letras")

	// Without RequireBody the stars alone are a rating.
	w := install(t)
	if r := rate(t, w, ana, camera, 4, ""); r.Body != "" {
		t.Fatalf("the text is %q", r.Body)
	}

	// The window an author may still edit in.
	window := installWith(t, func(c *rating.Config) { c.EditWindow = time.Hour })
	r := rate(t, window, ana, camera, 4, "x")
	window.clock.advance(30 * time.Minute)
	if _, err := window.ratings.Update(ctx, ana, r.ID, rating.RateRequest{Score: 5}); err != nil {
		t.Fatalf("editing inside the window: %v", err)
	}
	window.clock.advance(2 * time.Hour)
	_, err = window.ratings.Update(ctx, ana, r.ID, rating.RateRequest{Score: 1})
	if errs := rejected(t, err, "editing past the window"); len(errs.Get("score")) == 0 {
		t.Fatalf("the refusal is %v", errs)
	}
	detail, err := window.ratings.Find(ctx, ana, r.ID)
	if err != nil || detail.CanEdit || !detail.CanWithdraw {
		t.Fatalf("past the window the rating can be edited (%v) or cannot be deleted (%v), %v", detail.CanEdit, !detail.CanWithdraw, err)
	}
}

func TestARatingOfAnotherSpaceIsNotFound(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	theirs := rate(t, w, author("acme", "user-ana"), camera, 4, "da Acme")

	// A space manager of another account reads and changes nothing of Acme's.
	for _, stranger := range []security.Subject{author("hooli", "user-hu"), manager("hooli", "user-boss")} {
		if _, err := w.ratings.Find(ctx, stranger, theirs.ID); !errors.Is(err, rating.ErrNotFound) {
			t.Errorf("find by %s/%s: %v", stranger.Tenant, stranger.ID, err)
		}
		if _, err := w.ratings.Update(ctx, stranger, theirs.ID, rating.RateRequest{Score: 1}); !errors.Is(err, rating.ErrNotFound) {
			t.Errorf("update by %s/%s: %v", stranger.Tenant, stranger.ID, err)
		}
		if err := w.ratings.Withdraw(ctx, stranger, theirs.ID); !errors.Is(err, rating.ErrNotFound) {
			t.Errorf("withdraw by %s/%s: %v", stranger.Tenant, stranger.ID, err)
		}
		page, err := w.ratings.List(ctx, stranger, rating.ListQuery{})
		if err != nil || page.Total != 0 {
			t.Errorf("list by %s/%s: %d rows, %v", stranger.Tenant, stranger.ID, page.Total, err)
		}
	}
	// And the subject's owner is no author: it reads through its own side.
	if _, err := w.ratings.Find(ctx, owner("globex", "user-gil"), theirs.ID); !errors.Is(err, security.ErrForbidden) {
		t.Errorf("the owner reading through the author's side: %v", err)
	}
	if got, _ := w.ratings.Find(ctx, author("acme", "user-ana"), theirs.ID); got.Rating.Body != "da Acme" {
		t.Fatal("the author lost her own rating")
	}
	if _, err := w.ratings.Find(ctx, author("acme", "user-ana"), "no-such-rating"); !errors.Is(err, rating.ErrNotFound) {
		t.Fatalf("a rating that does not exist: %v", err)
	}
}

func TestAnAuthorReadsOnlyTheirOwnUnlessTheSpaceIsGranted(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana, bia, boss := author("acme", "user-ana"), author("acme", "user-bia"), manager("acme", "user-boss")

	anas := rate(t, w, ana, camera, 4, "da Ana")
	bias := rate(t, w, bia, alarm, 2, "da Bia")
	_ = bias

	if _, err := w.ratings.Find(ctx, bia, anas.ID); !errors.Is(err, rating.ErrNotFound) {
		t.Fatalf("a colleague read another's rating: %v", err)
	}
	if _, err := w.ratings.Update(ctx, bia, anas.ID, rating.RateRequest{Score: 1}); !errors.Is(err, rating.ErrNotFound) {
		t.Fatalf("a colleague changed another's rating: %v", err)
	}
	if err := w.ratings.Withdraw(ctx, bia, anas.ID); !errors.Is(err, rating.ErrNotFound) {
		t.Fatalf("a colleague deleted another's rating: %v", err)
	}
	if page, err := w.ratings.List(ctx, bia, rating.ListQuery{}); err != nil || page.Total != 1 || page.Rows[0].Rating.Body != "da Bia" {
		t.Fatalf("a colleague lists %d rows, %v", page.Total, err)
	}

	// The space's manager reads them all and may change them.
	if page, err := w.ratings.List(ctx, boss, rating.ListQuery{}); err != nil || page.Total != 2 {
		t.Fatalf("the manager lists %d rows, %v", page.Total, err)
	}
	if _, err := w.ratings.Update(ctx, boss, anas.ID, rating.RateRequest{Score: 3, Body: "revisada"}); err != nil {
		t.Fatalf("the manager changing a rating: %v", err)
	}
	if err := w.ratings.Withdraw(ctx, boss, anas.ID); err != nil {
		t.Fatalf("the manager deleting a rating: %v", err)
	}

	// Holding no action at all opens nothing.
	nobody := security.Subject{ID: "user-x", Tenant: "acme", Verified: true, Roles: []string{"admin"}}
	if _, err := w.ratings.List(ctx, nobody, rating.ListQuery{}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("a subject with no action lists: %v", err)
	}
	if _, err := w.ratings.Create(ctx, nobody, camera, rating.RateRequest{Score: 1}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("a subject with no action rates: %v", err)
	}
}

func TestTheListIsSearchedFilteredAndOrderedOnTheServer(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := author("acme", "user-ana")

	a := rate(t, w, ana, camera, 5, "excelente câmera")
	b := rate(t, w, ana, alarm, 2, "alarme ruim")
	c := rate(t, w, ana, plan, 3, "plano razoável")
	if err := w.ratings.Withdraw(ctx, ana, c.ID); err != nil {
		t.Fatal(err)
	}

	for name, q := range map[string]struct {
		query rating.ListQuery
		want  []string
	}{
		"nothing withdrawn":       {rating.ListQuery{Sort: "score", Dir: "asc"}, []string{b.ID, a.ID}},
		"by the text":             {rating.ListQuery{Search: "EXCELENTE"}, []string{a.ID}},
		"by the stars":            {rating.ListQuery{Scores: []int{2}}, []string{b.ID}},
		"by stars, highest first": {rating.ListQuery{Sort: "score"}, []string{a.ID, b.ID}},
		"by status":               {rating.ListQuery{Statuses: []rating.Status{rating.StatusRemoved}}, nil},
		"a page past the end":     {rating.ListQuery{Page: 9}, nil},
	} {
		page, err := w.ratings.List(ctx, ana, q.query)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got []string
		for _, row := range page.Rows {
			got = append(got, row.Rating.ID)
		}
		if strings.Join(got, ",") != strings.Join(q.want, ",") {
			t.Errorf("%s found %v, want %v", name, got, q.want)
		}
	}
	page, _ := w.ratings.List(ctx, ana, rating.ListQuery{Sort: "score", Dir: "asc"})
	if page.Rows[0].Subject.Name != "Alarme Sul" || page.Rows[0].Subject.URL != "" {
		t.Fatalf("the row names %+v; want the subject's name, and no address that is not the application's own or https", page.Rows[0].Subject)
	}
	for name, q := range map[string]rating.ListQuery{
		"a column outside the allowlist": {Sort: "author_id"},
		"a score that is not one":        {Scores: []int{9}},
		"a status that is not one":       {Statuses: []rating.Status{"bogus"}},
		"the withdrawn status":           {Statuses: []rating.Status{rating.StatusWithdrawn}},
	} {
		if _, err := w.ratings.List(ctx, ana, q); err == nil {
			t.Errorf("%s was accepted", name)
		} else {
			rejected(t, err, name)
		}
	}
}

func TestTheAverageCountsOnlyWhatIsPublished(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := author("acme", "user-ana")

	five := rate(t, w, ana, camera, 5, "")
	three := rate(t, w, author("hooli", "user-hu"), camera, 3, "")
	one := rate(t, w, author("umbrella", "user-u1"), camera, 1, "")
	reader := author("acme", "user-ana")

	summaries, err := w.ratings.Summaries(ctx, reader, []rating.Ref{camera, alarm})
	if err != nil || summaries[camera].Count != 3 || summaries[camera].Average != 3 || summaries[alarm].Count != 0 || summaries[alarm].Average != 0 {
		t.Fatalf("the summaries are %+v, %v", summaries, err)
	}

	// A rating that was reported counts until the moderation decides.
	filed := report(t, w, owner("globex", "user-gil"), one.ID, "A conta nunca usou.")
	if s, _ := w.ratings.Summaries(ctx, reader, []rating.Ref{camera}); s[camera].Count != 3 {
		t.Fatalf("a reported rating stopped counting before the decision: %+v", s[camera])
	}
	// A removed one does not.
	if _, err := w.moderation.Decide(ctx, moderator("mod-1"), filed.ID, rating.DecisionRequest{Verdict: rating.VerdictRemove, Note: "ofensivo"}); err != nil {
		t.Fatalf("removing: %v", err)
	}
	if s, _ := w.ratings.Summaries(ctx, reader, []rating.Ref{camera}); s[camera].Count != 2 || s[camera].Average != 4 {
		t.Fatalf("a removed rating counts: %+v", s[camera])
	}
	// Nor does one the author deleted.
	if err := w.ratings.Withdraw(ctx, author("hooli", "user-hu"), three.ID); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}
	if s, _ := w.ratings.Summaries(ctx, reader, []rating.Ref{camera}); s[camera].Count != 1 || s[camera].Average != 5 {
		t.Fatalf("a withdrawn rating counts: %+v", s[camera])
	}
	_ = five

	// The summary opens no text and is its own action.
	if _, err := w.ratings.Summaries(ctx, owner("globex", "user-gil"), nil); err != nil {
		t.Fatalf("a subject with the summary action was refused: %v", err)
	}
	if _, err := w.ratings.Summaries(ctx, security.Subject{ID: "u", Tenant: "acme", Verified: true, Actions: []security.Action{rating.RatingViewOwn}}, nil); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("reading the summary without the action: %v", err)
	}
	if _, err := w.ratings.Summaries(ctx, reader, []rating.Ref{{Type: "Bad", ID: "x"}}); err == nil {
		t.Fatal("a reference that cannot be one was summed")
	}
}

func TestTheLogShowsWhoDidWhatAndKeepsTheTextOutOfIt(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := author("acme", "user-ana")

	r := rate(t, w, ana, camera, 4, "SEGREDO-DO-TEXTO")
	if _, err := w.ratings.Update(ctx, ana, r.ID, rating.RateRequest{Score: 5, Body: "OUTRO-SEGREDO"}); err != nil {
		t.Fatal(err)
	}
	if err := w.ratings.Withdraw(ctx, ana, r.ID); err != nil {
		t.Fatal(err)
	}

	got := entries(t, w, "acme")
	want := []string{"rating.created", "rating.updated", "rating.withdrawn"}
	if len(got) != len(want) {
		t.Fatalf("the log holds %v", events(t, w, "acme"))
	}
	for i, e := range got {
		if e.Event != want[i] || e.Causer != "user-ana" || e.Subject != r.ID {
			t.Errorf("entry %d is %+v, want %s by user-ana about the rating", i, e, want[i])
		}
		if strings.Contains(e.Description+e.Properties, "SEGREDO") {
			t.Errorf("entry %d carries the text: %+v", i, e)
		}
	}
	if got[0].Description != "Avaliação de application/42 publicada" {
		t.Errorf("the description is %q", got[0].Description)
	}
}
