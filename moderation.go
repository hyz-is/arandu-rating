package rating

import (
	"context"
	"unicode/utf8"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"

	"github.com/hyz-is/arandu-rating/internal/platform"
)

// ModerationService orchestrates what the platform team does with the reports
// of every space.
//
// A moderator is a subject of Config.TeamTenant, and ModerationPolicy decides
// what they may do. The reports are in the owners' tenants and the ratings in
// the authors', so a use case here goes in steps: it asks the policy for a Grant
// in the team's own realm, reads the queue or finds the report's tenant with it,
// and then acts with a Grant scoped to each tenant it touches, which exists only
// because the first one was issued for the same action. Nothing a request names
// chooses a tenant: it is the one the rows say.
type ModerationService struct {
	*core
	policy ModerationPolicy
}

// NewModerationService wires the service over the application's database
// handle.
func NewModerationService(db *data.DB, cfg Config) *ModerationService {
	cfg = cfg.withDefaults()
	return &ModerationService{core: newCore(db, cfg), policy: ModerationPolicy{Realm: cfg.TeamTenant}}
}

// sortableQueue is the ordering allowlist of the moderation's queue.
var sortableQueue = map[string]string{
	"": "created_at", "created_at": "created_at", "decided_at": "decided_at", "status": "status",
	"reporter": "reporter_name", "subject": "subject_id",
}

// List returns a page of the reports of every space: searched by reason,
// reporter and subject, filtered by status and ordered by the server.
func (s *ModerationService) List(ctx context.Context, actor security.Subject, q ReportsQuery) (ReportPage, error) {
	g, err := security.Authorize(ctx, s.policy, actor, RatingViewAll, Report{})
	if err != nil {
		return ReportPage{}, err
	}
	column, ok := sortableQueue[q.Sort]
	if !ok {
		return ReportPage{}, s.labels.invalid("sort", "sort")
	}
	statuses, err := s.reportStatuses(q.Statuses)
	if err != nil {
		return ReportPage{}, err
	}
	perPage, page := s.paging(q.PerPage, q.Page)

	filtered := func() *platform.ReportQuery {
		b := platform.Reports(s.db)
		if len(statuses) > 0 {
			b = b.WhereIn("status", statuses)
		}
		matching(b.Base(), q.Search, "reason", "reporter_name", "subject_id")
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
	found, err := rows.OrderByDesc("id").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g, platform.ReportColumns...)
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
		out.Rows = append(out.Rows, ReportEntry{Report: reportViewOfPlatform(r), Subject: subject})
	}
	return out, nil
}

// Find returns one report with the rating as it stands, in whatever space they
// are: the moderation reads it all.
func (s *ModerationService) Find(ctx context.Context, actor security.Subject, id string) (ReportDetail, error) {
	at, err := s.reach(ctx, actor, RatingViewAll, id)
	if err != nil {
		return ReportDetail{}, err
	}
	read, err := scopedGrant(at.realm, RatingViewAll, at.report.RatingTenant, false)
	if err != nil {
		return ReportDetail{}, err
	}
	r, err := Ratings(s.db).WhereKey(at.report.RatingID).First(ctx, read)
	if err != nil {
		return ReportDetail{}, err
	}
	if r == nil {
		return ReportDetail{}, ErrNotFound
	}
	subject, err := s.describe(ctx, r.Subject(), map[Ref]Subject{})
	if err != nil {
		return ReportDetail{}, err
	}
	summary, err := s.summary(ctx, at.realm, r.Subject())
	if err != nil {
		return ReportDetail{}, err
	}
	return ReportDetail{Report: reportViewOf(at.report), Rating: ratingViewOf(r), Subject: subject, Summary: summary}, nil
}

// Decide closes an open report. The moderation removes the rating, corrects its
// text, or archives the report and changes nothing; whichever it does, the
// justification, who decided and when are kept, with the rating before and
// after. A decision is final: a report that was decided takes no other.
func (s *ModerationService) Decide(ctx context.Context, actor security.Subject, id string, in DecisionRequest) (*Report, error) {
	if errs := s.validateDecision(in); errs.Any() {
		return nil, errs
	}
	at, err := s.reach(ctx, actor, RatingModerate, id)
	if err != nil {
		return nil, err
	}
	if !at.report.IsOpen() {
		return nil, s.labels.invalid("verdict", "decided")
	}
	gRating, err := scopedGrant(at.realm, RatingModerate, at.report.RatingTenant, true)
	if err != nil {
		return nil, err
	}
	r, err := Ratings(s.db).WhereKey(at.report.RatingID).First(ctx, gRating)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrNotFound
	}

	who := s.person(ctx, s.cfg.TeamTenant, actor.ID)
	note := trimmed(in.Note)
	text := body(in.Body)
	now := s.now()
	event, outcome := ActivityReportDismissed, OutcomeNone
	scoreBefore := r.Score

	switch in.Verdict {
	case VerdictRemove:
		if err := r.Remove(actor.ID, note, now); err != nil {
			return nil, s.labels.invalid("verdict", "rating_changed")
		}
		event, outcome = ActivityRatingRemoved, OutcomeRemoved
		err = at.report.Uphold(outcome, actor.ID, who.Name, note, 0, "", now)
	case VerdictAlter:
		if text == r.Body && (in.Score == 0 || in.Score == r.Score) {
			return nil, s.labels.invalid("body", "unchanged")
		}
		if err := r.Alter(text, in.Score, actor.ID, note, now); err != nil {
			return nil, s.labels.invalid("verdict", "rating_changed")
		}
		event, outcome = ActivityRatingAltered, OutcomeAltered
		err = at.report.Uphold(outcome, actor.ID, who.Name, note, r.Score, r.Body, now)
	default:
		r.Release(now)
		err = at.report.Dismiss(actor.ID, who.Name, note, now)
	}
	if err != nil {
		return nil, s.labels.invalid("verdict", "decided")
	}

	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if _, err := r.Save(ctx, gRating); err != nil {
			return err
		}
		_, err := at.report.Save(ctx, at.grant)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, gRating, actor, event, r.ID, r.Subject(), map[string]any{
		"report": at.report.ID, "score_before": scoreBefore, "score_after": r.Score,
	}); err != nil {
		return nil, err
	}
	subject, err := s.describe(ctx, r.Subject(), map[Ref]Subject{})
	if err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventDecided, Subject: subject, Rating: ratingViewOf(r), Report: reportViewOf(at.report)})
	return at.report, nil
}

// reached is a report the moderation found, and the Grants to act on it.
type reached struct {
	report *Report
	// realm is the Grant the policy issued, in the team's own tenant.
	realm security.Grant
	// grant is the realm Grant scoped to the tenant the report is in.
	grant security.Grant
}

// reach finds the report a moderator acts on and the Grants to act on it with.
//
// The policy is asked twice, as it is for an author's reading: first for the
// kind of act, with nothing in hand, and then about the report itself. Between
// them the row is found in any tenant -- through the Global table, with the
// realm's Grant -- and its tenant is the one the work is scoped to.
func (s *ModerationService) reach(ctx context.Context, actor security.Subject, a security.Action, id string) (reached, error) {
	realm, err := security.Authorize(ctx, s.policy, actor, a, Report{})
	if err != nil {
		return reached{}, err
	}
	row, err := platform.Reports(s.db).WhereKey(id).First(ctx, realm, "id", "tenant_id")
	if err != nil {
		return reached{}, err
	}
	if row == nil {
		return reached{}, ErrNotFound
	}
	g, err := scopedGrant(realm, a, row.TenantID, !reads(a))
	if err != nil {
		return reached{}, err
	}
	r, err := Reports(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return reached{}, err
	}
	if r == nil {
		return reached{}, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, a, *r); err != nil {
		return reached{}, err
	}
	return reached{report: r, realm: realm, grant: g}, nil
}

// validateDecision checks a verdict, its justification and, for an alteration,
// the corrected text and stars.
func (c *core) validateDecision(in DecisionRequest) validation.Errors {
	errs := validation.Errors{}
	if !in.Verdict.Valid() {
		errs.Add("verdict", c.labels.message("verdict"))
	}
	note := trimmed(in.Note)
	switch {
	case note == "":
		errs.Add("note", c.labels.message("required"))
	case utf8.RuneCountInString(note) > c.cfg.MaxNoteChars:
		errs.Add("note", c.labels.message("max", map[string]any{"max": c.cfg.MaxNoteChars}))
	}
	if in.Verdict == VerdictAlter && utf8.RuneCountInString(body(in.Body)) > c.cfg.MaxBodyChars {
		errs.Add("body", c.labels.message("max", map[string]any{"max": c.cfg.MaxBodyChars}))
	}
	if in.Score != 0 {
		switch {
		case in.Verdict != VerdictAlter:
		case !c.cfg.ModerationMayChangeScore:
			errs.Add("score", c.labels.message("score_locked"))
		case !ValidScore(in.Score):
			errs.Add("score", c.labels.message("score"))
		}
	}
	return errs
}
