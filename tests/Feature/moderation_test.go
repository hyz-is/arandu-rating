package feature_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/security"

	rating "github.com/hyz-is/arandu-rating"
)

// a rating of Acme's of the camera, reported by Globex, ready to be decided.
func reported(t *testing.T, w world, score int, body string) (*rating.Rating, *rating.Report) {
	t.Helper()
	rated := rate(t, w, author("acme", "user-ana"), camera, score, body)
	return rated, report(t, w, owner("globex", "user-gil"), rated.ID, "A conta nunca usou o aplicativo.")
}

// state reads the rating and the report out of the database, as they are.
func state(t *testing.T, w world, ratingID, reportID string) (status string, review int, note, body string, score int, reportStatus string) {
	t.Helper()
	if err := w.sql.QueryRow(`SELECT status, under_review, moderation_note, body, score FROM ratings WHERE id = ?`, ratingID).Scan(&status, &review, &note, &body, &score); err != nil {
		t.Fatalf("reading the rating: %v", err)
	}
	if err := w.sql.QueryRow(`SELECT status FROM rating_reports WHERE id = ?`, reportID).Scan(&reportStatus); err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	return
}

func TestTheQueueListsEveryReportAndOnlyTheTeamReadsIt(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	mod := moderator("mod-1")

	_, first := reported(t, w, 1, "ruim")
	other := rate(t, w, author("hooli", "user-hu"), plan, 2, "caro")
	second := report(t, w, owner("initech", "user-i"), other.ID, "preço não é crítica ao serviço")

	page, err := w.moderation.List(ctx, mod, rating.ReportsQuery{Sort: "created_at", Dir: "asc"})
	if err != nil || page.Total != 2 || page.Rows[0].Report.ID != first.ID || page.Rows[1].Report.ID != second.ID {
		t.Fatalf("the queue holds %d reports, %v", page.Total, err)
	}
	// The moderation sees which spaces are involved, and the subject by name.
	if page.Rows[0].Report.Tenant != "globex" || page.Rows[0].Report.RatingTenant != "acme" || page.Rows[1].Subject.Name != "Plano Pro" {
		t.Fatalf("the rows are %+v", page.Rows)
	}
	for name, c := range map[string]struct {
		q    rating.ReportsQuery
		want []string
	}{
		"by the reason":   {rating.ReportsQuery{Search: "crítica"}, []string{second.ID}},
		"by the reporter": {rating.ReportsQuery{Search: "gil lopes"}, []string{first.ID}},
		"by the subject":  {rating.ReportsQuery{Search: "42"}, []string{first.ID}},
		"decided ones":    {rating.ReportsQuery{Statuses: []rating.ReportStatus{rating.ReportDismissed}}, nil},
		"newest first":    {rating.ReportsQuery{}, []string{second.ID, first.ID}},
	} {
		got, err := w.moderation.List(ctx, mod, c.q)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var ids []string
		for _, row := range got.Rows {
			ids = append(ids, row.Report.ID)
		}
		if strings.Join(ids, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s found %v, want %v", name, ids, c.want)
		}
	}
	if _, err := w.moderation.List(ctx, mod, rating.ReportsQuery{Sort: "rating_tenant"}); err == nil {
		t.Error("a column outside the allowlist was accepted")
	}

	// The detail: the report, the rating as it stands and the average.
	detail, err := w.moderation.Find(ctx, mod, first.ID)
	if err != nil || detail.Rating.Body != "ruim" || detail.Rating.Tenant != "acme" || detail.Report.Reason == "" || detail.Subject.Name != "Câmera Norte" || detail.Summary.Count != 1 {
		t.Fatalf("the detail is %+v, %v", detail, err)
	}
	if _, err := w.moderation.Find(ctx, mod, "no-such-report"); !errors.Is(err, rating.ErrNotFound) {
		t.Fatalf("a report that does not exist: %v", err)
	}

	// Nobody else reads the queue: not the customers, not the owner, not a
	// subject of another tenant holding the team's actions, not a team member
	// who was granted nothing.
	for name, who := range map[string]security.Subject{
		"an author":                    manager("acme", "user-boss"),
		"the owner":                    owner("globex", "user-gil"),
		"a team action elsewhere":      {ID: "mod-x", Tenant: "acme", Verified: true, Actions: rating.TeamActions()},
		"a team member with no action": {ID: "mod-y", Tenant: realm, Verified: true, Roles: []string{"admin"}},
	} {
		if _, err := w.moderation.List(ctx, who, rating.ReportsQuery{}); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("%s listed the queue: %v", name, err)
		}
		if _, err := w.moderation.Find(ctx, who, first.ID); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("%s read a report: %v", name, err)
		}
		if _, err := w.moderation.Decide(ctx, who, first.ID, rating.DecisionRequest{Verdict: rating.VerdictDismiss, Note: "x"}); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("%s decided a report: %v", name, err)
		}
	}
	// Reading is not deciding.
	readOnly := security.Subject{ID: "mod-r", Tenant: realm, Verified: true, Actions: []security.Action{rating.RatingViewAll}}
	if _, err := w.moderation.Find(ctx, readOnly, first.ID); err != nil {
		t.Fatalf("a team member who may read was refused: %v", err)
	}
	if _, err := w.moderation.Decide(ctx, readOnly, first.ID, rating.DecisionRequest{Verdict: rating.VerdictDismiss, Note: "x"}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("a team member who may only read decided: %v", err)
	}
}

func TestTheModerationRemovesARatingAndKeepsWhoWhenAndWhy(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rated, filed := reported(t, w, 1, "ofensa ao dono")
	ana, gil := author("acme", "user-ana"), owner("globex", "user-gil")

	w.clock.advance(time.Hour)
	decided, err := w.moderation.Decide(ctx, moderator("mod-1"), filed.ID, rating.DecisionRequest{Verdict: rating.VerdictRemove, Note: "Ofende a equipe do aplicativo."})
	if err != nil {
		t.Fatalf("removing: %v", err)
	}
	// The report keeps the decision: who, when, why, and the rating before and after.
	if decided.Status != rating.ReportUpheld || decided.Outcome != rating.OutcomeRemoved || decided.DecidedBy != "mod-1" || decided.DecidedByName != "Rita" ||
		decided.DecidedAt == nil || !decided.DecidedAt.After(filed.CreatedAt) || decided.DecisionNote != "Ofende a equipe do aplicativo." ||
		decided.ScoreBefore != 1 || decided.BodyBefore != "ofensa ao dono" || decided.ScoreAfter != 0 || decided.BodyAfter != "" {
		t.Fatalf("the report is %+v", decided)
	}
	status, review, note, body, _, reportStatus := state(t, w, rated.ID, filed.ID)
	// Nothing is deleted: the row says removed, and why.
	if status != "removed" || review != 0 || note != "Ofende a equipe do aplicativo." || body != "ofensa ao dono" || reportStatus != "upheld" {
		t.Fatalf("the rows say %q %d %q %q %q", status, review, note, body, reportStatus)
	}

	// The author reads the state and the reason, and cannot change or delete it.
	detail, err := w.ratings.Find(ctx, ana, rated.ID)
	if err != nil || detail.Rating.Status != rating.StatusRemoved || detail.Rating.ModerationNote != "Ofende a equipe do aplicativo." ||
		detail.Rating.ModeratedAt == nil || detail.CanEdit || detail.CanWithdraw {
		t.Fatalf("the author sees %+v, %v", detail, err)
	}
	if _, err := w.ratings.Update(ctx, ana, rated.ID, rating.RateRequest{Score: 5}); err == nil {
		t.Error("a removed rating was changed")
	}
	if err := w.ratings.Withdraw(ctx, ana, rated.ID); err == nil {
		t.Error("a removed rating was deleted")
	}
	// Nor can the account write it again.
	_, err = w.ratings.Create(ctx, ana, camera, rating.RateRequest{Score: 5, Body: "de novo"})
	if errs := rejected(t, err, "rating again after a removal"); len(errs.Get("score")) == 0 || !strings.Contains(errs.Get("score")[0], "removeu") {
		t.Fatalf("the refusal is %v", errs)
	}
	if page, _ := w.ratings.List(ctx, ana, rating.ListQuery{Statuses: []rating.Status{rating.StatusRemoved}}); page.Total != 1 {
		t.Fatalf("the author lists %d removed ratings", page.Total)
	}

	// The owner no longer reads it, and the average no longer counts it.
	if got, _ := w.owner.Received(ctx, gil, camera, rating.SubjectQuery{}); got.Page.Total != 0 || got.Summary.Count != 0 {
		t.Fatalf("the owner reads %d ratings with %d counted", got.Page.Total, got.Summary.Count)
	}
	// It follows the outcome, and the rating cannot be reported again.
	if _, err := w.owner.Report(ctx, gil, rated.ID, rating.ReportRequest{Reason: "de novo"}); !errors.Is(err, rating.ErrNotFound) && err == nil {
		t.Fatalf("a removed rating was reported again: %v", err)
	}

	// Everybody who should know is told; the log shows the moderator, in the
	// author's tenant, and keeps the text out.
	told := w.notices.all()
	if len(told) != 2 || told[1].Kind != rating.EventDecided || told[1].Kind.ForTeam() || told[1].Report.Outcome != rating.OutcomeRemoved ||
		told[1].Rating.Status != rating.StatusRemoved {
		t.Fatalf("the notifier was told %+v", told)
	}
	logged := entries(t, w, "acme")
	if got := events(t, w, "acme"); len(got) < 2 {
		t.Fatalf("the author's log holds %v", got)
	}
	last := logged[len(logged)-1]
	if last.Event != "rating.removed" || last.Causer != "mod-1" || last.Subject != rated.ID || strings.Contains(last.Properties+last.Description, "ofensa") {
		t.Fatalf("the last entry is %+v", last)
	}
}

func TestTheModerationAltersTheTextAndKeepsTheStars(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := author("acme", "user-ana")
	rated, filed := reported(t, w, 2, "Serviço lento e o dono é um idiota.")
	mod := moderator("mod-1")

	// The moderation corrects the text, not what somebody thought: the stars are
	// the author's unless the application says otherwise.
	_, err := w.moderation.Decide(ctx, mod, filed.ID, rating.DecisionRequest{Verdict: rating.VerdictAlter, Note: "x", Body: "Serviço lento.", Score: 4})
	if errs := rejected(t, err, "changing the stars"); len(errs.Get("score")) == 0 {
		t.Fatalf("the refusal is %v", errs)
	}
	// A correction that corrects nothing is refused, and so is one with no justification.
	_, err = w.moderation.Decide(ctx, mod, filed.ID, rating.DecisionRequest{Verdict: rating.VerdictAlter, Note: "x", Body: "Serviço lento e o dono é um idiota."})
	rejected(t, err, "altering to the same text")
	_, err = w.moderation.Decide(ctx, mod, filed.ID, rating.DecisionRequest{Verdict: rating.VerdictAlter, Body: "Serviço lento."})
	if errs := rejected(t, err, "deciding with no justification"); len(errs.Get("note")) == 0 {
		t.Fatalf("the refusal is %v", errs)
	}
	if status, review, _, _, _, reportStatus := state(t, w, rated.ID, filed.ID); status != "published" || review != 1 || reportStatus != "open" {
		t.Fatalf("a refused decision changed the rows: %q %d %q", status, review, reportStatus)
	}

	decided, err := w.moderation.Decide(ctx, mod, filed.ID, rating.DecisionRequest{Verdict: rating.VerdictAlter, Note: "Retirado o insulto.", Body: "Serviço lento."})
	if err != nil {
		t.Fatalf("altering: %v", err)
	}
	if decided.Status != rating.ReportUpheld || decided.Outcome != rating.OutcomeAltered || decided.BodyBefore != "Serviço lento e o dono é um idiota." ||
		decided.BodyAfter != "Serviço lento." || decided.ScoreBefore != 2 || decided.ScoreAfter != 2 || decided.DecisionNote != "Retirado o insulto." {
		t.Fatalf("the report is %+v", decided)
	}
	detail, err := w.ratings.Find(ctx, ana, rated.ID)
	if err != nil || detail.Rating.Body != "Serviço lento." || detail.Rating.Score != 2 || detail.Rating.Status != rating.StatusPublished ||
		detail.Rating.UnderReview || detail.Rating.ModerationNote != "Retirado o insulto." || !detail.CanEdit {
		t.Fatalf("the author sees %+v, %v", detail, err)
	}
	// It still counts, and the owner reads the corrected text only.
	got, _ := w.owner.Received(context.Background(), owner("globex", "user-gil"), camera, rating.SubjectQuery{})
	if got.Page.Total != 1 || got.Page.Rows[0].Rating.Body != "Serviço lento." || got.Summary.Count != 1 || got.Summary.Average != 2 {
		t.Fatalf("the owner reads %+v", got)
	}
	if got := events(t, w, "acme"); got[len(got)-1] != "rating.altered" {
		t.Fatalf("the author's log ends %v", got)
	}
}

func TestTheStarsAreChangedByTheModerationOnlyWhenTheApplicationLetsIt(t *testing.T) {
	t.Parallel()
	w := installWith(t, func(c *rating.Config) { c.ModerationMayChangeScore = true })
	ctx := context.Background()
	rated, filed := reported(t, w, 1, "tudo ruim, dono ladrão")
	mod := moderator("mod-1")

	_, err := w.moderation.Decide(ctx, mod, filed.ID, rating.DecisionRequest{Verdict: rating.VerdictAlter, Note: "x", Body: "tudo ruim", Score: 9})
	rejected(t, err, "stars that are not stars")
	decided, err := w.moderation.Decide(ctx, mod, filed.ID, rating.DecisionRequest{Verdict: rating.VerdictAlter, Note: "Nota e texto corrigidos.", Body: "tudo ruim", Score: 3})
	if err != nil || decided.ScoreBefore != 1 || decided.ScoreAfter != 3 {
		t.Fatalf("altering the stars: %v, %+v", err, decided)
	}
	if _, _, _, _, score, _ := state(t, w, rated.ID, filed.ID); score != 3 {
		t.Fatalf("the rating has %d stars", score)
	}
	if s, _ := w.ratings.Summaries(ctx, author("acme", "user-ana"), []rating.Ref{camera}); s[camera].Average != 3 {
		t.Fatalf("the average is %+v", s[camera])
	}
}

func TestTheModerationMayArchiveAReportAndChangeNothing(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana, gil := author("acme", "user-ana"), owner("globex", "user-gil")
	rated, filed := reported(t, w, 2, "não gostei")

	decided, err := w.moderation.Decide(ctx, moderator("mod-2"), filed.ID, rating.DecisionRequest{Verdict: rating.VerdictDismiss, Note: "É uma opinião, não uma ofensa."})
	if err != nil {
		t.Fatalf("dismissing: %v", err)
	}
	if decided.Status != rating.ReportDismissed || decided.Outcome != rating.OutcomeNone || decided.DecidedBy != "mod-2" || decided.DecidedByName != "Davi" ||
		decided.BodyAfter != decided.BodyBefore || decided.ScoreAfter != decided.ScoreBefore {
		t.Fatalf("the report is %+v", decided)
	}
	// The rating is exactly as it was, published, and free to be edited again;
	// the author is not told of a moderation that did nothing to them.
	detail, err := w.ratings.Find(ctx, ana, rated.ID)
	if err != nil || detail.Rating.Body != "não gostei" || detail.Rating.Score != 2 || detail.Rating.UnderReview || detail.Rating.ModerationNote != "" ||
		detail.Rating.ModeratedAt != nil || detail.Rating.Status != rating.StatusPublished || !detail.CanEdit {
		t.Fatalf("the author sees %+v, %v", detail, err)
	}
	if got := events(t, w, "acme"); got[len(got)-1] != "rating.report_dismissed" {
		t.Fatalf("the author's log ends %v", got)
	}

	// The owner can report it again if the rating later says something worse.
	if _, err := w.ratings.Update(ctx, ana, rated.ID, rating.RateRequest{Score: 1, Body: "pior ainda"}); err != nil {
		t.Fatalf("editing after the dismissal: %v", err)
	}
	second := report(t, w, gil, rated.ID, "agora é ofensa")
	if second.ID == filed.ID || second.BodyBefore != "pior ainda" {
		t.Fatalf("the second report is %+v", second)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM rating_reports WHERE rating_id = ?`, rated.ID); n != 2 {
		t.Fatalf("%d reports; want both kept", n)
	}
}

func TestADecisionIsFinalAndTouchesOnlyTheRowsOfThatReport(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	mod := moderator("mod-1")

	rated, filed := reported(t, w, 1, "ofensivo")
	// Another account's rating of the same subject, reported too, is not touched.
	bystander := rate(t, w, author("hooli", "user-hu"), camera, 5, "ótimo")
	bystandersReport := report(t, w, owner("globex", "user-gil"), bystander.ID, "motivo")

	if _, err := w.moderation.Decide(ctx, mod, filed.ID, rating.DecisionRequest{Verdict: rating.VerdictRemove, Note: "ofensa"}); err != nil {
		t.Fatalf("removing: %v", err)
	}
	// A second decision on the same report is refused, whoever asks.
	for _, verdict := range rating.Verdicts {
		_, err := w.moderation.Decide(ctx, moderator("mod-2"), filed.ID, rating.DecisionRequest{Verdict: verdict, Note: "outra", Body: "novo texto"})
		if errs := rejected(t, err, "deciding twice"); len(errs.Get("verdict")) == 0 {
			t.Errorf("deciding again with %s: %v", verdict, errs)
		}
	}
	if status, _, note, _, _, _ := state(t, w, rated.ID, filed.ID); status != "removed" || note != "ofensa" {
		t.Fatalf("a second decision changed the rating: %q %q", status, note)
	}
	if status, review, note, body, score, reportStatus := state(t, w, bystander.ID, bystandersReport.ID); status != "published" || review != 1 || note != "" ||
		body != "ótimo" || score != 5 || reportStatus != "open" {
		t.Fatalf("the other rating was touched: %q %d %q %q %d %q", status, review, note, body, score, reportStatus)
	}

	// The verdict, the justification and the report are all checked.
	_, err := w.moderation.Decide(ctx, mod, bystandersReport.ID, rating.DecisionRequest{Verdict: "erase", Note: "x"})
	if errs := rejected(t, err, "a verdict that is not one"); len(errs.Get("verdict")) == 0 {
		t.Fatalf("the refusal is %v", errs)
	}
	_, err = w.moderation.Decide(ctx, mod, bystandersReport.ID, rating.DecisionRequest{Verdict: rating.VerdictDismiss, Note: strings.Repeat("a", rating.DefaultMaxNoteChars+1)})
	rejected(t, err, "a justification too long")
	if _, err := w.moderation.Decide(ctx, mod, "no-such-report", rating.DecisionRequest{Verdict: rating.VerdictDismiss, Note: "x"}); !errors.Is(err, rating.ErrNotFound) {
		t.Fatalf("a report that does not exist: %v", err)
	}
	if _, _, _, _, _, reportStatus := state(t, w, bystander.ID, bystandersReport.ID); reportStatus != "open" {
		t.Fatal("a refused decision closed the report")
	}
}

func TestANotifierThatFailsDoesNotUndoWhatWasStored(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	w.notices.fail = errors.New("the mail server is down")

	rated := rate(t, w, author("acme", "user-ana"), camera, 1, "ofensivo")
	filed, err := w.owner.Report(ctx, owner("globex", "user-gil"), rated.ID, rating.ReportRequest{Reason: "ofende"})
	if err != nil {
		t.Fatalf("the report failed because the mail did: %v", err)
	}
	if _, err := w.moderation.Decide(ctx, moderator("mod-1"), filed.ID, rating.DecisionRequest{Verdict: rating.VerdictRemove, Note: "ofensa"}); err != nil {
		t.Fatalf("the decision failed because the mail did: %v", err)
	}
	if status, _, _, _, _, reportStatus := state(t, w, rated.ID, filed.ID); status != "removed" || reportStatus != "upheld" {
		t.Fatalf("the rows say %q %q", status, reportStatus)
	}
	logged := w.logs.String()
	if !strings.Contains(logged, "the notifier failed") || !strings.Contains(logged, "rating.decided") || strings.Contains(logged, "ofensivo") {
		t.Fatalf("the failure is logged as %q", logged)
	}
}
