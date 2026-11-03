package rating

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"

	"github.com/hyz-is/arandu-rating/internal/platform"
)

// OwnerService orchestrates what the owner of a subject does with the ratings
// it received: reading them, reporting one, and following the reports it filed.
//
// The ratings are in other accounts' tenants, so a read here goes through the
// table that reads every tenant -- after OwnerPolicy answered, and after the
// application said the subject is the account's -- and a report is written in
// the owner's own tenant, with the one write it makes in the rating's tenant
// done under a Grant scoped to it. Nothing a request names chooses the tenant of
// either: it is the Grant's, or the one the rating's row says.
type OwnerService struct {
	*core
	policy OwnerPolicy
}

// NewOwnerService wires the service over the application's database handle.
func NewOwnerService(db *data.DB, cfg Config) *OwnerService {
	return &OwnerService{core: newCore(db, cfg.withDefaults()), policy: OwnerPolicy{}}
}

// sortableOwned is the ordering allowlist of the owner's subjects.
var sortableOwned = map[string]bool{"": true, "name": true, "average": true, "count": true}

// Subjects returns the subjects the account owns, with what their ratings add
// up to: searched by name and ordered by the server.
func (s *OwnerService) Subjects(ctx context.Context, actor security.Subject, q OwnedQuery) (OwnedPage, error) {
	g, err := security.Authorize(ctx, s.policy, actor, RatingViewSubject, Target{})
	if err != nil {
		return OwnedPage{}, err
	}
	if !sortableOwned[q.Sort] {
		return OwnedPage{}, s.labels.invalid("sort", "sort")
	}
	tenant := data.Tenant(g)
	owned, err := s.cfg.Subjects.Owned(ctx, tenant)
	if err != nil {
		return OwnedPage{}, err
	}
	term := strings.ToLower(trimmed(q.Search))
	rows := make([]OwnedEntry, 0, len(owned))
	for _, subject := range owned {
		// The application answers who owns what, and the package keeps only what
		// is the account's: a subject another tenant owns is not listed here
		// whatever the answer held.
		if !subject.Ref.Valid() || subject.OwnerTenant != tenant {
			continue
		}
		if subject.Name = clip(trimmed(subject.Name), maxNameLen); subject.Name == "" {
			subject.Name = subject.Ref.String()
		}
		subject.URL = safeURL(subject.URL)
		if term != "" && !strings.Contains(strings.ToLower(subject.Name), term) && !strings.Contains(strings.ToLower(subject.Ref.ID), term) {
			continue
		}
		rows = append(rows, OwnedEntry{Subject: subject})
	}

	perPage, page := s.paging(q.PerPage, q.Page)
	// Ordering by what the ratings add up to needs the sums of every subject; by
	// name, only those of the page that is shown.
	if q.Sort == "average" || q.Sort == "count" {
		for i := range rows {
			if rows[i].Summary, err = s.summary(ctx, g, rows[i].Subject.Ref); err != nil {
				return OwnedPage{}, err
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		x, y := rows[i], rows[j]
		cmp := 0
		switch q.Sort {
		case "average":
			cmp = compareFloat(x.Summary.Average, y.Summary.Average)
		case "count":
			cmp = x.Summary.Count - y.Summary.Count
		}
		if cmp == 0 {
			cmp = strings.Compare(strings.ToLower(x.Subject.Name), strings.ToLower(y.Subject.Name))
		}
		if q.Dir == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	out := OwnedPage{Total: len(rows), Page: page, Pages: pagesOf(len(rows), perPage)}
	from := (page - 1) * perPage
	if from > len(rows) {
		from = len(rows)
	}
	to := from + perPage
	if to > len(rows) {
		to = len(rows)
	}
	out.Rows = rows[from:to]
	if q.Sort != "average" && q.Sort != "count" {
		for i := range out.Rows {
			if out.Rows[i].Summary, err = s.summary(ctx, g, out.Rows[i].Subject.Ref); err != nil {
				return OwnedPage{}, err
			}
		}
	}
	return out, nil
}

// sortableSubjectRatings is the ordering allowlist of a subject's ratings.
var sortableSubjectRatings = map[string]string{
	"": "created_at", "created_at": "created_at", "score": "score", "author": "author_name",
}

// Received returns the published ratings of a subject the account owns, with
// what they add up to: searched, filtered by stars and ordered by the server. A
// subject the account does not own is not found, whatever the application
// knows about it.
func (s *OwnerService) Received(ctx context.Context, actor security.Subject, ref Ref, q SubjectQuery) (SubjectRatings, error) {
	g, subject, err := s.owns(ctx, actor, RatingViewSubject, ref)
	if err != nil {
		return SubjectRatings{}, err
	}
	column, ok := sortableSubjectRatings[q.Sort]
	if !ok {
		return SubjectRatings{}, s.labels.invalid("sort", "sort")
	}
	scores, err := s.scores(q.Scores)
	if err != nil {
		return SubjectRatings{}, err
	}
	perPage, page := s.paging(q.PerPage, q.Page)

	filtered := func() *platform.RatingQuery {
		b := platform.Ratings(s.db).Where("subject_type", "=", ref.Type).Where("subject_id", "=", ref.ID).
			Where("status", "=", string(StatusPublished))
		if len(scores) > 0 {
			b = b.WhereIn("score", scores)
		}
		matching(b.Base(), q.Search, "body", "author_name")
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return SubjectRatings{}, err
	}
	rows := filtered()
	if q.Dir == "asc" {
		rows = rows.OrderBy(column)
	} else {
		rows = rows.OrderByDesc(column)
	}
	found, err := rows.OrderByDesc("id").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g, platform.RatingColumns...)
	if err != nil {
		return SubjectRatings{}, err
	}
	out := SubjectRatings{Subject: subject, Page: RatingPage{
		Rows: make([]RatingEntry, 0, len(found)), Total: int(total), Page: page, Pages: pagesOf(int(total), perPage),
	}}
	for _, r := range found {
		out.Page.Rows = append(out.Page.Rows, RatingEntry{Rating: ratingViewOfPlatform(r), Subject: subject})
	}
	if out.Summary, err = s.summary(ctx, g, ref); err != nil {
		return SubjectRatings{}, err
	}
	return out, nil
}

// owns authorizes the act and finds the subject, which has to be the account's.
//
// The policy is asked twice, as it is for an author's reading: first for the
// kind of act, with nothing in hand, and then about the subject itself, with the
// owner the application named. A subject the account does not own is answered as
// not found, as a rating of another space is: telling an account that a subject
// exists and is somebody else's tells them what exists.
func (s *OwnerService) owns(ctx context.Context, actor security.Subject, a security.Action, ref Ref) (security.Grant, Subject, error) {
	g, err := security.Authorize(ctx, s.policy, actor, a, Target{})
	if err != nil {
		return security.Grant{}, Subject{}, err
	}
	if !ref.Valid() {
		return security.Grant{}, Subject{}, ErrNotFound
	}
	subject, err := s.subject(ctx, ref)
	if err != nil {
		return security.Grant{}, Subject{}, err
	}
	if _, err := security.Authorize(ctx, s.policy, actor, a, Target{Subject: ref, Owner: subject.OwnerTenant}); err != nil {
		return security.Grant{}, Subject{}, ErrNotFound
	}
	return g, subject, nil
}

// reach finds the rating the owner acts on, in whatever tenant it is, and the
// subject it is about, which has to be the account's.
func (s *OwnerService) reach(ctx context.Context, actor security.Subject, a security.Action, id string) (*platform.Rating, Subject, security.Grant, error) {
	g, err := security.Authorize(ctx, s.policy, actor, a, Target{})
	if err != nil {
		return nil, Subject{}, security.Grant{}, err
	}
	row, err := platform.Ratings(s.db).WhereKey(id).First(ctx, g, platform.RatingColumns...)
	if err != nil {
		return nil, Subject{}, security.Grant{}, err
	}
	if row == nil || Status(row.Status) == StatusWithdrawn {
		return nil, Subject{}, security.Grant{}, ErrNotFound
	}
	ref := Ref{Type: row.SubjectType, ID: row.SubjectID}
	subject, err := s.subject(ctx, ref)
	if err != nil {
		return nil, Subject{}, security.Grant{}, err
	}
	if _, err := security.Authorize(ctx, s.policy, actor, a, Target{Subject: ref, Owner: subject.OwnerTenant}); err != nil {
		return nil, Subject{}, security.Grant{}, ErrNotFound
	}
	return row, subject, g, nil
}

// Rating returns one rating of a subject the account owns, for the form that
// reports it. A rating that is not published carries no text.
func (s *OwnerService) Rating(ctx context.Context, actor security.Subject, id string) (RatingEntry, error) {
	row, subject, _, err := s.reach(ctx, actor, RatingViewSubject, id)
	if err != nil {
		return RatingEntry{}, err
	}
	return RatingEntry{Rating: ratingViewOfPlatform(row), Subject: subject}, nil
}

// Report files a report on a rating of a subject the account owns, with the
// reason the owner gives. The rating stays published and counted until the
// moderation decides, and it takes no change from its author in the meantime.
// A rating can have one report open at a time.
func (s *OwnerService) Report(ctx context.Context, actor security.Subject, ratingID string, in ReportRequest) (*Report, error) {
	if errs := s.validateReport(in); errs.Any() {
		return nil, errs
	}
	row, subject, g, err := s.reach(ctx, actor, RatingReport, ratingID)
	if err != nil {
		return nil, err
	}
	switch {
	case Status(row.Status) != StatusPublished:
		return nil, s.labels.invalid("reason", "not_reportable")
	case row.UnderReview != 0:
		return nil, s.labels.invalid("reason", "already_reported")
	}
	// The one write outside the owner's own tenant: the mark on the rating that
	// says a report is open. It is scoped to the rating's tenant, for this act.
	scoped, err := scopedGrant(g, RatingReport, row.TenantID, true)
	if err != nil {
		return nil, err
	}
	who := s.person(ctx, data.Tenant(g), actor.ID)
	now := s.now()
	var filed *Report
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		// The mark is taken by a statement that only succeeds on a rating no one
		// else has reported, so two reports at once are told apart by the
		// database and not by a read.
		marked, err := Ratings(s.db).WhereKey(row.ID).Where("status", "=", string(StatusPublished)).Where("under_review", "=", 0).
			Update(ctx, scoped, map[string]any{"under_review": 1, "updated_at": now})
		if err != nil {
			return err
		}
		if marked == 0 {
			return s.labels.invalid("reason", "already_reported")
		}
		report, err := Reports(s.db).New()
		if err != nil {
			return err
		}
		if report.ID, err = data.NewID(); err != nil {
			return err
		}
		report.TenantID = data.Tenant(g)
		report.RatingID, report.RatingTenant = row.ID, row.TenantID
		report.SubjectType, report.SubjectID = row.SubjectType, row.SubjectID
		report.ReporterID, report.ReporterName = actor.ID, who.Name
		report.Reason = clip(trimmed(in.Reason), s.cfg.MaxReasonChars)
		report.Status = ReportOpen
		report.ScoreBefore, report.BodyBefore = row.Score, row.Body
		report.CreatedAt, report.UpdatedAt = now, now
		if _, err := report.Save(ctx, g); err != nil {
			return err
		}
		filed = report
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, ActivityRatingReported, row.ID, Ref{Type: row.SubjectType, ID: row.SubjectID}, map[string]any{"report": filed.ID}); err != nil {
		return nil, err
	}
	rating := ratingViewOfPlatform(row)
	rating.UnderReview = true
	s.notify(ctx, Event{Kind: EventReported, Subject: subject, Rating: rating, Report: reportViewOf(filed)})
	return filed, nil
}

// sortableOwnerReports is the ordering allowlist of the reports an account
// filed.
var sortableOwnerReports = map[string]string{
	"": "created_at", "created_at": "created_at", "decided_at": "decided_at", "status": "status", "subject": "subject_id",
}

// Filed returns the reports the account filed, with what the moderation
// decided about each: searched, filtered by status and ordered by the server.
func (s *OwnerService) Filed(ctx context.Context, actor security.Subject, q ReportsQuery) (ReportPage, error) {
	g, err := security.Authorize(ctx, s.policy, actor, RatingViewSubject, Target{})
	if err != nil {
		return ReportPage{}, err
	}
	column, ok := sortableOwnerReports[q.Sort]
	if !ok {
		return ReportPage{}, s.labels.invalid("sort", "sort")
	}
	statuses, err := s.reportStatuses(q.Statuses)
	if err != nil {
		return ReportPage{}, err
	}
	perPage, page := s.paging(q.PerPage, q.Page)

	filtered := func() *ReportQuery {
		b := Reports(s.db)
		if len(statuses) > 0 {
			b = b.WhereIn("status", statuses)
		}
		matching(b.Base(), q.Search, "reason", "subject_id", "decision_note")
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return ReportPage{}, err
	}
	rows := filtered()
	if q.Dir == "asc" {
		rows = rows.OrderBy(column)
	} else {
		rows = rows.OrderByDesc(column)
	}
	found, err := rows.OrderByDesc("id").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g)
	if err != nil {
		return ReportPage{}, err
	}
	out := ReportPage{Rows: make([]ReportEntry, 0, len(found)), Total: int(total), Page: page, Pages: pagesOf(int(total), perPage)}
	seen := map[Ref]Subject{}
	for _, r := range found {
		subject, err := s.describe(ctx, Ref{Type: r.SubjectType, ID: r.SubjectID}, seen)
		if err != nil {
			return ReportPage{}, err
		}
		view := reportViewOf(r)
		// What the owner follows is what was decided about their report. The
		// moderator who decided it, and the other account, stay on the other
		// side.
		view.RatingTenant, view.DecidedBy, view.DecidedByName = "", "", ""
		out.Rows = append(out.Rows, ReportEntry{Report: view, Subject: subject})
	}
	return out, nil
}

// validateReport checks the reason of a report.
func (c *core) validateReport(in ReportRequest) validation.Errors {
	errs := validation.Errors{}
	reason := trimmed(in.Reason)
	switch {
	case reason == "":
		errs.Add("reason", c.labels.message("required"))
	case utf8.RuneCountInString(reason) > c.cfg.MaxReasonChars:
		errs.Add("reason", c.labels.message("max", map[string]any{"max": c.cfg.MaxReasonChars}))
	}
	return errs
}

// reportStatuses turns the statuses a request named into the values a statement
// compares with, refusing one that is not a status.
func (c *core) reportStatuses(in []ReportStatus) ([]any, error) {
	out := make([]any, 0, len(in))
	for _, s := range in {
		if !s.Valid() {
			return nil, c.labels.invalid("status", "status")
		}
		out = append(out, string(s))
	}
	return out, nil
}

// compareFloat orders two numbers: negative, zero or positive.
func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
