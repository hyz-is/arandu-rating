package rating

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/hesape/database/model"

	"github.com/hyz-is/arandu-rating/internal/platform"
)

// The table names, declared once: the migrations and the models read them from
// here, so a rename is one edit.
const (
	ratingsTable = "ratings"
	reportsTable = "rating_reports"
)

// Column widths, declared once for the migrations and the validation, so a
// value the form accepts is a value the column holds.
const (
	maxIdentifierLen = 64
	maxRefLen        = 64
	maxNameLen       = 160
)

// The scale of a rating: one star to five. It is the product's decision and not
// a setting, because an average of ratings given on two different scales is a
// number that means nothing.
const (
	MinScore = 1
	MaxScore = 5
)

// Ref names the thing that is rated: a kind the application chose ("application",
// "plan", "partner") and the identifier of one of them. The package knows
// nothing else about it; what a subject is called, who owns it and who may rate
// it are the application's, answered through Subjects and Eligibility.
type Ref struct {
	Type string
	ID   string
}

// Valid reports whether the reference can be stored and put in an address: a
// kind of lowercase letters, digits, dots, dashes and underscores, and an
// identifier of letters, digits and those characters.
func (r Ref) Valid() bool { return validRefPart(r.Type, true) && validRefPart(r.ID, false) }

// IsZero reports whether the reference names nothing.
func (r Ref) IsZero() bool { return r.Type == "" && r.ID == "" }

// String is the reference as one line: "application/42".
func (r Ref) String() string { return r.Type + "/" + r.ID }

func validRefPart(value string, kind bool) bool {
	if value == "" || len(value) > maxRefLen {
		return false
	}
	for i, c := range value {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '.' || c == '-' || c == '_':
			if i == 0 {
				return false
			}
		case !kind && (c >= 'A' && c <= 'Z' || c == ':'):
		default:
			return false
		}
	}
	return true
}

// Status is where a rating stands.
type Status string

// The statuses of a rating.
const (
	// StatusPublished is a rating everybody who may read the subject's ratings
	// reads, and the only one the average counts.
	StatusPublished Status = "published"
	// StatusRemoved is a rating the moderation took down. Its author still
	// reads it, and the reason; nobody else does, and the average does not count
	// it. It cannot be written again.
	StatusRemoved Status = "removed"
	// StatusWithdrawn is a rating its author deleted. The text is gone, the row
	// stays so the author can rate again, and nobody reads it.
	StatusWithdrawn Status = "withdrawn"
)

// Statuses are every status, in the order the screens list them.
var Statuses = []Status{StatusPublished, StatusRemoved, StatusWithdrawn}

// Valid reports whether the status is one of the three.
func (s Status) Valid() bool {
	for _, known := range Statuses {
		if s == known {
			return true
		}
	}
	return false
}

// ReportStatus is where a report stands.
type ReportStatus string

// The statuses of a report.
const (
	// ReportOpen is a report nobody on the moderation has decided yet.
	ReportOpen ReportStatus = "open"
	// ReportUpheld is a report the moderation agreed with: the rating was
	// removed or altered.
	ReportUpheld ReportStatus = "upheld"
	// ReportDismissed is a report the moderation archived without changing
	// anything.
	ReportDismissed ReportStatus = "dismissed"
)

// ReportStatuses are every status of a report, in the order the screens list
// them.
var ReportStatuses = []ReportStatus{ReportOpen, ReportUpheld, ReportDismissed}

// Valid reports whether the status is one of the three.
func (s ReportStatus) Valid() bool {
	for _, known := range ReportStatuses {
		if s == known {
			return true
		}
	}
	return false
}

// Outcome is what an upheld report did to its rating.
type Outcome string

// The outcomes of a report.
const (
	// OutcomeNone is a report still open, or one that was dismissed.
	OutcomeNone Outcome = ""
	// OutcomeRemoved is a report whose rating was taken down.
	OutcomeRemoved Outcome = "removed"
	// OutcomeAltered is a report whose rating stays, with its text corrected.
	OutcomeAltered Outcome = "altered"
)

// Verdict is what the moderation decides about an open report.
type Verdict string

// The verdicts of the moderation.
const (
	// VerdictRemove takes the rating down.
	VerdictRemove Verdict = "remove"
	// VerdictAlter corrects the text of the rating and keeps it.
	VerdictAlter Verdict = "alter"
	// VerdictDismiss archives the report and changes nothing.
	VerdictDismiss Verdict = "dismiss"
)

// Verdicts are every verdict, in the order the screens list them.
var Verdicts = []Verdict{VerdictRemove, VerdictAlter, VerdictDismiss}

// Valid reports whether the verdict is one of the three.
func (v Verdict) Valid() bool {
	for _, known := range Verdicts {
		if v == known {
			return true
		}
	}
	return false
}

// Flag is a yes-or-no a column holds as an integer, read back from whatever an
// engine answers with: the engines disagree about booleans and one place has
// to flatten them.
type Flag bool

// Value writes the flag as the integer the column holds.
func (f Flag) Value() (driver.Value, error) {
	if f {
		return int64(1), nil
	}
	return int64(0), nil
}

// Scan reads the flag back. Anything it cannot read is refused rather than
// read as false.
func (f *Flag) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*f = false
	case bool:
		*f = Flag(v)
	case int64:
		*f = v != 0
	case int32:
		*f = v != 0
	case int:
		*f = v != 0
	case float64:
		*f = v != 0
	case []byte:
		return f.Scan(string(v))
	case string:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("rating: %q is not a yes-or-no", v)
		}
		*f = Flag(b)
	default:
		return fmt.Errorf("rating: cannot read %T as a yes-or-no", value)
	}
	return nil
}

// Rating is one account's evaluation of one subject: stars, and a text if it
// wanted one.
//
// It embeds the model, so a row returned by a query carries the connection and
// can be saved again. Build a new row with Ratings(db).New(): a struct literal
// has no connection and its write methods return model.ErrUnwired.
//
// Ratings, RatingQuery and RatingCollection are generated beside this file, in
// RatingQuery.go, by aru model:build.
type Rating struct {
	model.Model

	// ID is the identifier. It is generated by the application rather than by
	// the database, because a DEFAULT that produces a uuid is spelled
	// differently in every engine.
	ID string `db:"id"`

	// TenantID is the account that gave the rating: the author's space, and the
	// tenant of the row. It is written from the Grant and read back so the
	// policy can compare it against the subject's tenant. An account rates a
	// subject once, whoever in it wrote the text.
	TenantID string `db:"tenant_id"`

	// SubjectType and SubjectID are what was rated.
	SubjectType string `db:"subject_type"`
	SubjectID   string `db:"subject_id"`

	// AuthorID is the user of the tenant who wrote it. The application owns
	// what a user is; this package only compares the identifier with the
	// subject's.
	AuthorID string `db:"author_id"`

	// AuthorName is who the author was when they rated, as the application's
	// Directory answered. It is a copy: the owner of the subject reads it
	// without asking the application again.
	AuthorName string `db:"author_name"`

	// Score is the stars, from MinScore to MaxScore.
	Score int `db:"score"`

	// Body is the text, plain, or empty.
	Body string `db:"body"`

	// Status is where the rating stands.
	Status Status `db:"status"`

	// UnderReview says a report on this rating is open. The rating keeps being
	// published and counted until the moderation decides.
	UnderReview Flag `db:"under_review"`

	// ModerationNote is why the moderation removed or altered the rating, as
	// the moderator wrote it for the author. Empty when it never did.
	ModerationNote string `db:"moderation_note"`

	// ModeratedAt and ModeratedBy are when and by whom, nil and empty when it
	// never happened.
	ModeratedAt *time.Time `db:"moderated_at"`
	ModeratedBy string     `db:"moderated_by"`

	// EditedAt is when the author last changed the rating, nil while they have
	// not.
	EditedAt *time.Time `db:"edited_at"`

	// CreatedAt is when the row was written, in UTC.
	CreatedAt time.Time `db:"created_at"`

	// UpdatedAt is when the row last changed, in UTC.
	UpdatedAt time.Time `db:"updated_at"`
}

// ratingTable is the table Rating is a row of, declared once. Its query,
// Ratings, is generated beside it and is the one way to reach the rows.
//
// ManualKey because the primary key is text the application writes: the
// database neither increments it nor fills it. NoTimestamps because the clock
// is Config.Now. The tenant scope is left at its tenant_id default.
var ratingTable = model.NewTable(model.TableSpec{
	Name:         ratingsTable,
	New:          func() model.Entity { return new(Rating) },
	ManualKey:    true,
	NoTimestamps: true,
})

// arandu:begin custom

// Subject is what was rated.
func (r Rating) Subject() Ref { return Ref{Type: r.SubjectType, ID: r.SubjectID} }

// IsPublished reports whether the rating is the one everybody reads and the
// average counts.
func (r Rating) IsPublished() bool { return r.Status == StatusPublished }

// CanEdit reports whether the author may change the rating now: it is
// published, no report on it is open, and the window the application allows
// has not passed. A zero window never closes.
func (r Rating) CanEdit(now time.Time, window time.Duration) bool {
	if r.Status != StatusPublished || bool(r.UnderReview) {
		return false
	}
	return window <= 0 || now.Sub(r.CreatedAt) <= window
}

// CanWithdraw reports whether the author may delete the rating: it is
// published and no report on it is open.
func (r Rating) CanWithdraw() bool { return r.Status == StatusPublished && !bool(r.UnderReview) }

// CanBeReported reports whether the owner of the subject may report it: it is
// published and nobody has an open report on it already.
func (r Rating) CanBeReported() bool { return r.Status == StatusPublished && !bool(r.UnderReview) }

// Revise changes the stars and the text, and stamps the change.
func (r *Rating) Revise(score int, body string, now time.Time, window time.Duration) error {
	if !r.CanEdit(now, window) {
		return fmt.Errorf("rating: %s cannot be edited now", r.ID)
	}
	if !ValidScore(score) {
		return fmt.Errorf("rating: %d is not a score", score)
	}
	now = now.UTC()
	r.Score, r.Body = score, body
	r.EditedAt = &now
	r.UpdatedAt = now
	return nil
}

// Withdraw deletes what the author wrote. The row stays, without the text, so
// the account can rate the subject again.
func (r *Rating) Withdraw(now time.Time) error {
	if !r.CanWithdraw() {
		return fmt.Errorf("rating: %s cannot be withdrawn now", r.ID)
	}
	r.Status = StatusWithdrawn
	r.Body = ""
	r.UpdatedAt = now.UTC()
	return nil
}

// CanRenew reports whether the author may rate again over this row: they
// withdrew the earlier one.
func (r Rating) CanRenew() bool { return r.Status == StatusWithdrawn }

// Renew gives a withdrawn rating new stars and a new text, as if it had just
// been written.
func (r *Rating) Renew(score int, body, authorID, authorName string, now time.Time) error {
	if !r.CanRenew() {
		return fmt.Errorf("rating: %s was not withdrawn", r.ID)
	}
	if !ValidScore(score) {
		return fmt.Errorf("rating: %d is not a score", score)
	}
	now = now.UTC()
	r.Status = StatusPublished
	r.Score, r.Body = score, body
	r.AuthorID, r.AuthorName = authorID, authorName
	r.UnderReview = false
	r.ModerationNote, r.ModeratedAt, r.ModeratedBy = "", nil, ""
	r.EditedAt = nil
	r.CreatedAt, r.UpdatedAt = now, now
	return nil
}

// Remove takes the rating down, for the reason the moderator wrote for the
// author. The report on it is closed by the same act.
func (r *Rating) Remove(by, note string, now time.Time) error {
	if r.Status != StatusPublished {
		return fmt.Errorf("rating: %s is %s and cannot be removed", r.ID, r.Status)
	}
	now = now.UTC()
	r.Status = StatusRemoved
	r.UnderReview = false
	r.ModerationNote, r.ModeratedAt, r.ModeratedBy = note, &now, by
	r.UpdatedAt = now
	return nil
}

// Alter corrects the text, and the stars only when score is not zero; the
// rating stays published. The report on it is closed by the same act.
func (r *Rating) Alter(body string, score int, by, note string, now time.Time) error {
	if r.Status != StatusPublished {
		return fmt.Errorf("rating: %s is %s and cannot be altered", r.ID, r.Status)
	}
	if score != 0 && !ValidScore(score) {
		return fmt.Errorf("rating: %d is not a score", score)
	}
	now = now.UTC()
	r.Body = body
	if score != 0 {
		r.Score = score
	}
	r.UnderReview = false
	r.ModerationNote, r.ModeratedAt, r.ModeratedBy = note, &now, by
	r.UpdatedAt = now
	return nil
}

// Release closes the review without changing the rating.
func (r *Rating) Release(now time.Time) {
	r.UnderReview = false
	r.UpdatedAt = now.UTC()
}

// ValidScore reports whether n is a number of stars.
func ValidScore(n int) bool { return n >= MinScore && n <= MaxScore }

// arandu:end custom

// ErrNotFound is returned when no row matches, including when the row exists
// in another tenant. The two cases are deliberately indistinguishable.
//
// It wraps model.ErrModelNotFound, so the router answers it with 404 and a
// caller can still ask for it by name with errors.Is.
var ErrNotFound = fmt.Errorf("rating: record not found: %w", model.ErrModelNotFound)

// Report is the owner of a subject saying a rating should not stand, and what
// the moderation decided about it.
//
// Its tenant is the reporter's: the owner follows their own reports with their
// own Grant, and the moderation reads them all through the platform table.
// RatingTenant says where the rating is, so the moderation acts in that tenant
// under a Grant scoped to it.
type Report struct {
	model.Model

	// ID is the identifier.
	ID string `db:"id"`

	// TenantID is the reporter's tenant: the owner of the subject.
	TenantID string `db:"tenant_id"`

	// RatingID and RatingTenant say which rating, and the tenant it is in.
	RatingID     string `db:"rating_id"`
	RatingTenant string `db:"rating_tenant"`

	// SubjectType and SubjectID are copied from the rating so the queue lists
	// by subject without opening the rating.
	SubjectType string `db:"subject_type"`
	SubjectID   string `db:"subject_id"`

	// ReporterID and ReporterName are who reported, as the application knew
	// them.
	ReporterID   string `db:"reporter_id"`
	ReporterName string `db:"reporter_name"`

	// Reason is why the owner disagrees.
	Reason string `db:"reason"`

	// Status is where the report stands, and Outcome what an upheld one did.
	Status  ReportStatus `db:"status"`
	Outcome Outcome      `db:"outcome"`

	// DecidedBy, DecidedByName, DecidedAt and DecisionNote are who decided,
	// when, and the justification they wrote. Empty while the report is open.
	DecidedBy     string     `db:"decided_by"`
	DecidedByName string     `db:"decided_by_name"`
	DecidedAt     *time.Time `db:"decided_at"`
	DecisionNote  string     `db:"decision_note"`

	// ScoreBefore and BodyBefore are the rating as it was reported. ScoreAfter
	// and BodyAfter are what remained after the decision: the corrected text of
	// an alteration, nothing for a removal, the same for a dismissal.
	ScoreBefore int    `db:"score_before"`
	BodyBefore  string `db:"body_before"`
	ScoreAfter  int    `db:"score_after"`
	BodyAfter   string `db:"body_after"`

	// CreatedAt is when it was reported, in UTC.
	CreatedAt time.Time `db:"created_at"`

	// UpdatedAt is when the row last changed, in UTC.
	UpdatedAt time.Time `db:"updated_at"`
}

// reportTable is the table Report is a row of.
var reportTable = model.NewTable(model.TableSpec{
	Name:         reportsTable,
	New:          func() model.Entity { return new(Report) },
	ManualKey:    true,
	NoTimestamps: true,
})

// IsOpen reports whether nobody decided the report yet.
func (r Report) IsOpen() bool { return r.Status == ReportOpen }

// Uphold records that the moderation agreed: the rating was removed or altered.
// What remained of it is kept beside what was reported.
func (r *Report) Uphold(outcome Outcome, by, byName, note string, scoreAfter int, bodyAfter string, now time.Time) error {
	if !r.IsOpen() {
		return fmt.Errorf("rating: report %s is %s and takes no decision", r.ID, r.Status)
	}
	if outcome != OutcomeRemoved && outcome != OutcomeAltered {
		return fmt.Errorf("rating: %q is not an outcome", outcome)
	}
	now = now.UTC()
	r.Status, r.Outcome = ReportUpheld, outcome
	r.DecidedBy, r.DecidedByName, r.DecidedAt, r.DecisionNote = by, byName, &now, note
	r.ScoreAfter, r.BodyAfter = scoreAfter, bodyAfter
	r.UpdatedAt = now
	return nil
}

// Dismiss records that the moderation archived the report and changed nothing.
func (r *Report) Dismiss(by, byName, note string, now time.Time) error {
	if !r.IsOpen() {
		return fmt.Errorf("rating: report %s is %s and takes no decision", r.ID, r.Status)
	}
	now = now.UTC()
	r.Status, r.Outcome = ReportDismissed, OutcomeNone
	r.DecidedBy, r.DecidedByName, r.DecidedAt, r.DecisionNote = by, byName, &now, note
	r.ScoreAfter, r.BodyAfter = r.ScoreBefore, r.BodyBefore
	r.UpdatedAt = now
	return nil
}

// Summary is what the published ratings of a subject add up to.
type Summary struct {
	// Count is how many published ratings there are. Removed and withdrawn ones
	// are not among them.
	Count int
	// Average is their mean, zero when there are none.
	Average float64
}

// Rounded is the average to one decimal, the way it is shown.
func (s Summary) Rounded() float64 {
	return float64(int(s.Average*10+0.5)) / 10
}

// Stars is the average rounded to the nearest whole star, for a read-only
// rating group. Zero when nobody rated.
func (s Summary) Stars() int {
	if s.Count == 0 {
		return 0
	}
	n := int(s.Average + 0.5)
	if n < MinScore {
		return MinScore
	}
	if n > MaxScore {
		return MaxScore
	}
	return n
}

// summaryOf turns a count and a sum into a Summary.
func summaryOf(count int64, sum float64) Summary {
	if count <= 0 {
		return Summary{}
	}
	return Summary{Count: int(count), Average: sum / float64(count)}
}

// RatingView is a rating as a list, a response or a notification carries it:
// plain values, with no connection to the database.
type RatingView struct {
	ID             string
	Tenant         string
	Subject        Ref
	AuthorID       string
	AuthorName     string
	Score          int
	Body           string
	Status         Status
	UnderReview    bool
	ModerationNote string
	ModeratedAt    *time.Time
	EditedAt       *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ratingViewOf snapshots a rating.
func ratingViewOf(r *Rating) RatingView {
	if r == nil {
		return RatingView{}
	}
	return RatingView{
		ID: r.ID, Tenant: r.TenantID, Subject: r.Subject(), AuthorID: r.AuthorID, AuthorName: r.AuthorName,
		Score: r.Score, Body: r.Body, Status: r.Status, UnderReview: bool(r.UnderReview),
		ModerationNote: r.ModerationNote, ModeratedAt: r.ModeratedAt, EditedAt: r.EditedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// ratingViewOfPlatform snapshots a rating read across tenants. A rating that is
// not published carries no text out of its tenant: what the moderation took
// down, and what the author deleted, are read by nobody else.
func ratingViewOfPlatform(r *platform.Rating) RatingView {
	if r == nil {
		return RatingView{}
	}
	view := RatingView{
		ID: r.ID, Tenant: r.TenantID, Subject: Ref{Type: r.SubjectType, ID: r.SubjectID}, AuthorID: r.AuthorID,
		AuthorName: r.AuthorName, Score: r.Score, Status: Status(r.Status), UnderReview: r.UnderReview != 0,
		EditedAt: r.EditedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if view.Status == StatusPublished {
		view.Body = r.Body
	}
	return view
}

// ReportView is a report as a list, a response or a notification carries it.
type ReportView struct {
	ID            string
	Tenant        string
	RatingID      string
	RatingTenant  string
	Subject       Ref
	ReporterID    string
	ReporterName  string
	Reason        string
	Status        ReportStatus
	Outcome       Outcome
	DecidedBy     string
	DecidedByName string
	DecidedAt     *time.Time
	DecisionNote  string
	ScoreBefore   int
	BodyBefore    string
	ScoreAfter    int
	BodyAfter     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// reportViewOf snapshots a report.
func reportViewOf(r *Report) ReportView {
	if r == nil {
		return ReportView{}
	}
	return ReportView{
		ID: r.ID, Tenant: r.TenantID, RatingID: r.RatingID, RatingTenant: r.RatingTenant,
		Subject:    Ref{Type: r.SubjectType, ID: r.SubjectID},
		ReporterID: r.ReporterID, ReporterName: r.ReporterName, Reason: r.Reason, Status: r.Status, Outcome: r.Outcome,
		DecidedBy: r.DecidedBy, DecidedByName: r.DecidedByName, DecidedAt: r.DecidedAt, DecisionNote: r.DecisionNote,
		ScoreBefore: r.ScoreBefore, BodyBefore: r.BodyBefore, ScoreAfter: r.ScoreAfter, BodyAfter: r.BodyAfter,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// reportViewOfPlatform snapshots a report the moderation's queue read.
func reportViewOfPlatform(r *platform.Report) ReportView {
	if r == nil {
		return ReportView{}
	}
	return ReportView{
		ID: r.ID, Tenant: r.TenantID, RatingID: r.RatingID, RatingTenant: r.RatingTenant,
		Subject:    Ref{Type: r.SubjectType, ID: r.SubjectID},
		ReporterID: r.ReporterID, ReporterName: r.ReporterName, Reason: r.Reason, Status: ReportStatus(r.Status), Outcome: Outcome(r.Outcome),
		DecidedBy: r.DecidedBy, DecidedByName: r.DecidedByName, DecidedAt: r.DecidedAt, DecisionNote: r.DecisionNote,
		ScoreBefore: r.ScoreBefore, BodyBefore: r.BodyBefore, ScoreAfter: r.ScoreAfter, BodyAfter: r.BodyAfter,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// fieldsResource is a response made of named fields.
type fieldsResource map[string]any

// ToArray returns the fields.
func (f fieldsResource) ToArray() map[string]any { return f }

// With returns what goes beside the fields: nothing.
func (fieldsResource) With() map[string]any { return nil }

func timeField(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// authorFields are the fields a rating answers its author with. The tenant is
// not among them, and neither is anything about who reported it.
func authorFields(v RatingView) map[string]any {
	return map[string]any{
		"id":              v.ID,
		"subject_type":    v.Subject.Type,
		"subject_id":      v.Subject.ID,
		"score":           v.Score,
		"body":            v.Body,
		"status":          string(v.Status),
		"under_review":    v.UnderReview,
		"moderation_note": v.ModerationNote,
		"moderated_at":    timeField(v.ModeratedAt),
		"edited_at":       timeField(v.EditedAt),
		"created_at":      v.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":      v.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// ownerFields are the fields a rating answers the owner of its subject with:
// what was said and who said it by name, and nothing that identifies the
// author's account.
func ownerFields(v RatingView) map[string]any {
	return map[string]any{
		"id":           v.ID,
		"subject_type": v.Subject.Type,
		"subject_id":   v.Subject.ID,
		"author_name":  v.AuthorName,
		"score":        v.Score,
		"body":         v.Body,
		"under_review": v.UnderReview,
		"created_at":   v.CreatedAt.UTC().Format(time.RFC3339),
		"edited_at":    timeField(v.EditedAt),
	}
}

// ownerReportFields are the fields a report answers its reporter with.
func ownerReportFields(v ReportView) map[string]any {
	return map[string]any{
		"id":            v.ID,
		"rating_id":     v.RatingID,
		"subject_type":  v.Subject.Type,
		"subject_id":    v.Subject.ID,
		"reason":        v.Reason,
		"status":        string(v.Status),
		"outcome":       string(v.Outcome),
		"decision_note": v.DecisionNote,
		"decided_at":    timeField(v.DecidedAt),
		"created_at":    v.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// teamReportFields are the fields a report answers the moderation with: the
// tenants too, and the rating before and after.
func teamReportFields(v ReportView) map[string]any {
	out := ownerReportFields(v)
	out["tenant"] = v.Tenant
	out["rating_tenant"] = v.RatingTenant
	out["reporter_id"] = v.ReporterID
	out["reporter_name"] = v.ReporterName
	out["decided_by"] = v.DecidedBy
	out["decided_by_name"] = v.DecidedByName
	out["score_before"] = v.ScoreBefore
	out["body_before"] = v.BodyBefore
	out["score_after"] = v.ScoreAfter
	out["body_after"] = v.BodyAfter
	return out
}

func summaryFields(s Summary) map[string]any {
	return map[string]any{"count": s.Count, "average": s.Rounded()}
}

// trimmed is the text with the space around it taken off.
func trimmed(s string) string { return strings.TrimSpace(s) }

// clip cuts text to at most n characters.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
