package feature_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/auth"

	rating "github.com/hyz-is/arandu-rating"
)

func TestTheOwnerListsItsSubjectsWithTheirAverages(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	gil := owner("globex", "user-gil")

	rate(t, w, author("acme", "user-ana"), camera, 5, "")
	rate(t, w, author("hooli", "user-hu"), camera, 3, "")
	rate(t, w, author("acme", "user-ana"), plan, 1, "")

	page, err := w.owner.Subjects(ctx, gil, rating.OwnedQuery{})
	if err != nil || page.Total != 2 {
		t.Fatalf("the owner lists %d subjects, %v; want its two", page.Total, err)
	}
	// By name: the alarm, then the camera; the plan is another space's.
	if page.Rows[0].Subject.Name != "Alarme Sul" || page.Rows[1].Subject.Name != "Câmera Norte" {
		t.Fatalf("the rows are %+v", page.Rows)
	}
	if page.Rows[0].Summary.Count != 0 || page.Rows[1].Summary.Count != 2 || page.Rows[1].Summary.Average != 4 {
		t.Fatalf("the summaries are %+v and %+v", page.Rows[0].Summary, page.Rows[1].Summary)
	}
	// The address the application gave that is not its own or https is dropped.
	if page.Rows[0].Subject.URL != "" || page.Rows[1].Subject.URL != "/catalogo/42" {
		t.Fatalf("the addresses are %q and %q", page.Rows[0].Subject.URL, page.Rows[1].Subject.URL)
	}

	for name, c := range map[string]struct {
		q    rating.OwnedQuery
		want []string
	}{
		"by the highest average": {rating.OwnedQuery{Sort: "average", Dir: "desc"}, []string{"Câmera Norte", "Alarme Sul"}},
		"by the lowest average":  {rating.OwnedQuery{Sort: "average", Dir: "asc"}, []string{"Alarme Sul", "Câmera Norte"}},
		"by the most ratings":    {rating.OwnedQuery{Sort: "count", Dir: "desc"}, []string{"Câmera Norte", "Alarme Sul"}},
		"by name, backwards":     {rating.OwnedQuery{Dir: "desc"}, []string{"Câmera Norte", "Alarme Sul"}},
		"by a search":            {rating.OwnedQuery{Search: "CÂMERA"}, []string{"Câmera Norte"}},
		"by an identifier":       {rating.OwnedQuery{Search: "43"}, []string{"Alarme Sul"}},
		"past the last page":     {rating.OwnedQuery{Page: 5}, nil},
	} {
		got, err := w.owner.Subjects(ctx, gil, c.q)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var names []string
		for _, row := range got.Rows {
			names = append(names, row.Subject.Name)
		}
		if strings.Join(names, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s found %v, want %v", name, names, c.want)
		}
	}
	if _, err := w.owner.Subjects(ctx, gil, rating.OwnedQuery{Sort: "owner_tenant"}); err == nil {
		t.Error("a column outside the allowlist was accepted")
	}

	// Initech owns the plan and nothing else; an account that owns nothing sees
	// nothing, and an author is no owner.
	if got, _ := w.owner.Subjects(ctx, owner("initech", "user-i"), rating.OwnedQuery{}); got.Total != 1 || got.Rows[0].Subject.Name != "Plano Pro" {
		t.Fatalf("initech lists %+v", got.Rows)
	}
	if got, _ := w.owner.Subjects(ctx, owner("acme", "user-o"), rating.OwnedQuery{}); got.Total != 0 {
		t.Fatalf("an account that owns nothing lists %d subjects", got.Total)
	}
	if _, err := w.owner.Subjects(ctx, author("globex", "user-gil"), rating.OwnedQuery{}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("an author without the owner's action lists: %v", err)
	}
}

func TestTheOwnerReadsTheRatingsOfItsSubjectAndNoOtherAndOnlyThePublishedOnes(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	gil := owner("globex", "user-gil")

	a := rate(t, w, author("acme", "user-ana"), camera, 5, "excelente")
	b := rate(t, w, author("hooli", "user-hu"), camera, 2, "ruim")
	gone := rate(t, w, author("umbrella", "user-u1"), camera, 1, "apagada")
	if err := w.ratings.Withdraw(ctx, author("umbrella", "user-u1"), gone.ID); err != nil {
		t.Fatal(err)
	}
	rate(t, w, author("acme", "user-ana"), plan, 4, "do outro dono")

	got, err := w.owner.Received(ctx, gil, camera, rating.SubjectQuery{Sort: "score", Dir: "asc"})
	if err != nil || got.Page.Total != 2 || got.Summary.Count != 2 || got.Subject.Name != "Câmera Norte" {
		t.Fatalf("the owner reads %d ratings, %v", got.Page.Total, err)
	}
	if got.Page.Rows[0].Rating.ID != b.ID || got.Page.Rows[0].Rating.AuthorName != "Hugo Reis" || got.Page.Rows[1].Rating.Body != "excelente" {
		t.Fatalf("the rows are %+v", got.Page.Rows)
	}
	_ = a

	for name, q := range map[string]struct {
		q    rating.SubjectQuery
		want int
	}{
		"by the stars":  {rating.SubjectQuery{Scores: []int{5}}, 1},
		"by the text":   {rating.SubjectQuery{Search: "RUIM"}, 1},
		"by the author": {rating.SubjectQuery{Search: "souza"}, 1},
		"by nothing":    {rating.SubjectQuery{Search: "nada disso"}, 0},
	} {
		page, err := w.owner.Received(ctx, gil, camera, q.q)
		if err != nil || page.Page.Total != q.want {
			t.Errorf("%s found %d, %v; want %d", name, page.Page.Total, err, q.want)
		}
	}

	// A subject another account owns, one nobody knows and one that cannot be
	// one are all not found; the answer never says which.
	for name, ref := range map[string]rating.Ref{"another's": plan, "unknown": ghost, "malformed": {Type: "X", ID: "../y"}} {
		if _, err := w.owner.Received(ctx, gil, ref, rating.SubjectQuery{}); !errors.Is(err, rating.ErrNotFound) {
			t.Errorf("reading the ratings of a subject that is %s: %v", name, err)
		}
	}
	// Nor does an account that rated it, or any other, read them as the owner.
	for _, stranger := range []security.Subject{owner("acme", "user-o"), owner("initech", "user-i")} {
		if _, err := w.owner.Received(ctx, stranger, camera, rating.SubjectQuery{}); !errors.Is(err, rating.ErrNotFound) {
			t.Errorf("%s read the ratings of a subject it does not own: %v", stranger.Tenant, err)
		}
	}
}

func TestTheOwnerReportsARatingAndItGoesUnderReview(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana, gil := author("acme", "user-ana"), owner("globex", "user-gil")

	rated := rate(t, w, ana, camera, 2, "mentira, nunca usei")
	filed := report(t, w, gil, rated.ID, "  A conta nunca conectou a câmera.  ")

	// The report is the owner's: its tenant, the reporter and the rating as it was.
	if filed.TenantID != "globex" || filed.RatingTenant != "acme" || filed.RatingID != rated.ID || filed.ReporterID != "user-gil" ||
		filed.ReporterName != "Gil Lopes" || filed.Reason != "A conta nunca conectou a câmera." || filed.Status != rating.ReportOpen ||
		filed.ScoreBefore != 2 || filed.BodyBefore != "mentira, nunca usei" || filed.SubjectType != "application" || filed.SubjectID != "42" {
		t.Fatalf("the report is %+v", filed)
	}

	// The rating is the author's, in the author's tenant, and says it is under
	// review; it is still published and still counts.
	detail, err := w.ratings.Find(ctx, ana, rated.ID)
	if err != nil || !detail.Rating.UnderReview || detail.Rating.Status != rating.StatusPublished || detail.CanEdit || detail.CanWithdraw {
		t.Fatalf("the author sees %+v, %v", detail, err)
	}
	if _, err := w.ratings.Update(ctx, ana, rated.ID, rating.RateRequest{Score: 5, Body: "mudei de ideia"}); err == nil {
		t.Fatal("a rating under review was changed")
	} else if errs := rejected(t, err, "editing under review"); len(errs.Get("score")) == 0 || !strings.Contains(errs.Get("score")[0], "em análise") {
		t.Fatalf("the refusal is %v", errs)
	}
	if err := w.ratings.Withdraw(ctx, ana, rated.ID); err == nil {
		t.Fatal("a rating under review was deleted")
	}
	got, err := w.owner.Received(ctx, gil, camera, rating.SubjectQuery{})
	if err != nil || got.Page.Total != 1 || !got.Page.Rows[0].Rating.UnderReview || got.Summary.Count != 1 {
		t.Fatalf("the owner reads %+v, %v", got.Page, err)
	}

	// One report at a time.
	_, err = w.owner.Report(ctx, gil, rated.ID, rating.ReportRequest{Reason: "de novo"})
	if errs := rejected(t, err, "a second report"); len(errs.Get("reason")) == 0 || !strings.Contains(errs.Get("reason")[0], "já foi denunciada") {
		t.Fatalf("the refusal is %v", errs)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM rating_reports`); n != 1 {
		t.Fatalf("%d reports; want one", n)
	}

	// The moderation is told, with the subject and the reason, and the log shows
	// the owner's act in the owner's own tenant.
	told := w.notices.all()
	if len(told) != 1 || told[0].Kind != rating.EventReported || !told[0].Kind.ForTeam() || told[0].Subject.Name != "Câmera Norte" ||
		told[0].Report.Reason != "A conta nunca conectou a câmera." || !told[0].Rating.UnderReview {
		t.Fatalf("the notifier was told %+v", told)
	}
	logged := entries(t, w, "globex")
	if len(logged) != 1 || logged[0].Event != "rating.reported" || logged[0].Causer != "user-gil" || logged[0].Subject != rated.ID ||
		strings.Contains(logged[0].Properties, "conectou") {
		t.Fatalf("the owner's log is %+v", logged)
	}
}

func TestOnlyTheOwnerOfTheSubjectReports(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rated := rate(t, w, author("acme", "user-ana"), camera, 1, "ruim")
	reason := rating.ReportRequest{Reason: "motivo"}

	for name, who := range map[string]security.Subject{
		"the author's own account":     owner("acme", "user-ana"),
		"an account that owns nothing": owner("hooli", "user-hu"),
		"the owner of another subject": owner("initech", "user-i"),
	} {
		if _, err := w.owner.Report(ctx, who, rated.ID, reason); !errors.Is(err, rating.ErrNotFound) {
			t.Errorf("%s reported the rating: %v", name, err)
		}
	}
	// A member of the owner's space who was not granted the action does not.
	if _, err := w.owner.Report(ctx, security.Subject{ID: "u", Tenant: "globex", Verified: true, Actions: []security.Action{rating.RatingViewSubject}}, rated.ID, reason); !errors.Is(err, security.ErrForbidden) {
		t.Errorf("a member without the action reported: %v", err)
	}
	if _, err := w.owner.Report(ctx, owner("globex", "user-gil"), "no-such-rating", reason); !errors.Is(err, rating.ErrNotFound) {
		t.Errorf("a rating that does not exist was reported: %v", err)
	}
	if _, err := w.owner.Rating(ctx, owner("hooli", "user-hu"), rated.ID); !errors.Is(err, rating.ErrNotFound) {
		t.Errorf("a stranger read the rating through the report form: %v", err)
	}

	// The reason is required and bounded, before anything is written.
	for name, in := range map[string]rating.ReportRequest{
		"no reason":      {},
		"a blank reason": {Reason: "   "},
		"a long reason":  {Reason: strings.Repeat("a", rating.DefaultMaxReasonChars+1)},
	} {
		_, err := w.owner.Report(ctx, owner("globex", "user-gil"), rated.ID, in)
		if errs := rejected(t, err, name); len(errs.Get("reason")) == 0 {
			t.Errorf("%s: the refusal is %v", name, errs)
		}
	}
	if n := count(t, w, `SELECT COUNT(*) FROM rating_reports`); n != 0 {
		t.Fatalf("%d reports were written by refusals", n)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM ratings WHERE under_review = 1`); n != 0 {
		t.Fatalf("%d ratings were put under review by refusals", n)
	}

	// A rating that is not published is not reported.
	gone := rate(t, w, author("hooli", "user-hu"), camera, 2, "apagar")
	if err := w.ratings.Withdraw(ctx, author("hooli", "user-hu"), gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.owner.Report(ctx, owner("globex", "user-gil"), gone.ID, reason); !errors.Is(err, rating.ErrNotFound) {
		t.Errorf("a withdrawn rating was reported: %v", err)
	}
}

func TestAnOwnerSomebodyIsOnlyViewingAsWritesNothingAndOneSomebodyActsAsWritesAsThem(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rated := rate(t, w, author("acme", "user-ana"), camera, 1, "ruim")
	gil := owner("globex", "user-gil")
	support := security.Subject{ID: "support-1", Tenant: realm, Verified: true}

	viewing, err := auth.Impersonate(support, gil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.owner.Report(ctx, viewing, rated.ID, rating.ReportRequest{Reason: "motivo"}); err == nil {
		t.Fatal("an operator only viewing as the owner reported a rating")
	}
	if n := count(t, w, `SELECT COUNT(*) FROM rating_reports`) + count(t, w, `SELECT COUNT(*) FROM ratings WHERE under_review = 1`); n != 0 {
		t.Fatalf("a view-only session changed %d rows", n)
	}
	// Viewing reads: the ratings of the subject are there to look at.
	if got, err := w.owner.Received(ctx, viewing, camera, rating.SubjectQuery{}); err != nil || got.Page.Total != 1 {
		t.Fatalf("an operator viewing as the owner reads %d ratings, %v", got.Page.Total, err)
	}

	acting, err := auth.ImpersonateActing(support, gil)
	if err != nil {
		t.Fatal(err)
	}
	filed := report(t, w, acting, rated.ID, "motivo")
	if filed.TenantID != "globex" || filed.ReporterID != "user-gil" {
		t.Fatalf("the report is %+v", filed)
	}
	// The log names the operator beside the account they acted in.
	if got := entries(t, w, "globex"); len(got) != 1 || got[0].Event != "rating.reported" || got[0].Causer != "user-gil" ||
		!strings.Contains(got[0].Properties, "support-1") || !strings.Contains(got[0].Properties, "acting") {
		t.Fatalf("the log is %+v", got)
	}
}

func TestTheOwnerFollowsItsReportsAndOnlyItsOwn(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	gil := owner("globex", "user-gil")

	one := rate(t, w, author("acme", "user-ana"), camera, 1, "ofensivo")
	two := rate(t, w, author("hooli", "user-hu"), alarm, 2, "bom assunto")
	first := report(t, w, gil, one.ID, "ofende a equipe")
	report(t, w, gil, two.ID, "não usou")
	if _, err := w.moderation.Decide(ctx, moderator("mod-1"), first.ID, rating.DecisionRequest{Verdict: rating.VerdictRemove, Note: "ofensa comprovada"}); err != nil {
		t.Fatalf("deciding: %v", err)
	}

	page, err := w.owner.Filed(ctx, gil, rating.ReportsQuery{Sort: "created_at", Dir: "asc"})
	if err != nil || page.Total != 2 {
		t.Fatalf("the owner follows %d reports, %v", page.Total, err)
	}
	decided, waiting := page.Rows[0].Report, page.Rows[1].Report
	if decided.Status != rating.ReportUpheld || decided.Outcome != rating.OutcomeRemoved || decided.DecisionNote != "ofensa comprovada" || decided.DecidedAt == nil ||
		waiting.Status != rating.ReportOpen || waiting.DecisionNote != "" {
		t.Fatalf("the reports are %+v and %+v", decided, waiting)
	}
	// What the owner follows is the outcome, not who decided it or where the
	// rating lives.
	if decided.DecidedBy != "" || decided.DecidedByName != "" || decided.RatingTenant != "" {
		t.Fatalf("the owner's report carries the moderator or the other account: %+v", decided)
	}
	if page.Rows[0].Subject.Name != "Câmera Norte" {
		t.Fatalf("the row names %+v", page.Rows[0].Subject)
	}

	for name, c := range map[string]struct {
		q    rating.ReportsQuery
		want int
	}{
		"open ones":    {rating.ReportsQuery{Statuses: []rating.ReportStatus{rating.ReportOpen}}, 1},
		"upheld ones":  {rating.ReportsQuery{Statuses: []rating.ReportStatus{rating.ReportUpheld}}, 1},
		"by the text":  {rating.ReportsQuery{Search: "OFENDE"}, 1},
		"by the notes": {rating.ReportsQuery{Search: "comprovada"}, 1},
	} {
		if got, err := w.owner.Filed(ctx, gil, c.q); err != nil || got.Total != c.want {
			t.Errorf("%s found %d, %v; want %d", name, got.Total, err, c.want)
		}
	}
	if _, err := w.owner.Filed(ctx, gil, rating.ReportsQuery{Statuses: []rating.ReportStatus{"bogus"}}); err == nil {
		t.Error("a status that is not one was accepted")
	}

	// Another owner follows its own reports: none.
	if got, _ := w.owner.Filed(ctx, owner("initech", "user-i"), rating.ReportsQuery{}); got.Total != 0 {
		t.Fatalf("another owner follows %d of globex's reports", got.Total)
	}
	// The same account's other member follows them too: the reports are the
	// space's.
	if got, _ := w.owner.Filed(ctx, owner("globex", "user-other"), rating.ReportsQuery{}); got.Total != 2 {
		t.Fatalf("another member of the owner's space follows %d reports; want the space's two", got.Total)
	}
}

func TestTwoReportsAtOnceFileOneAndTheOtherIsToldSo(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rated := rate(t, w, author("acme", "user-ana"), camera, 1, "ruim")

	const attempts = 8
	var wg sync.WaitGroup
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.owner.Report(ctx, owner("globex", "user-gil"), rated.ID, rating.ReportRequest{Reason: "motivo"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	filed, refused := 0, 0
	for err := range results {
		var errs validation.Errors
		switch {
		case err == nil:
			filed++
		case errors.As(err, &errs) && len(errs.Get("reason")) > 0:
			refused++
		default:
			t.Errorf("a report failed in a way nobody expected: %v", err)
		}
	}
	if filed != 1 || refused != attempts-1 {
		t.Fatalf("%d reports were filed and %d refused out of %d at once; want exactly one filed", filed, refused, attempts)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM rating_reports WHERE rating_id = ?`, rated.ID); n != 1 {
		t.Fatalf("%d reports are stored; want one", n)
	}
}
