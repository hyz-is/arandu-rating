// Package platform is the ratings and the reports read without the tenant
// filter: the reads of arandu-rating that cross tenants. The owner of a subject
// reads the ratings other accounts gave it, the moderation reads every report,
// and an average counts every account's rating.
//
// It is internal so no application can import it. Its tables are Global, which
// is how Hesape spells a table with no tenant filter, and any Grant at all
// would read every tenant through them; the package that may reach them is the
// one whose callers have asked a policy first. Its rows never leave them: they
// are copied into snapshots that hold no connection, so nothing read across
// tenants can be saved or deleted through it, and every write goes through the
// tenant's own model under a Grant scoped to that tenant.
package platform

import (
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// Rating is a row of the ratings table, of any tenant. It declares the columns
// a list or an average shows; the moderation's own notes stay in the tenant's
// model.
type Rating struct {
	model.Model

	ID          string     `db:"id"`
	TenantID    string     `db:"tenant_id"`
	SubjectType string     `db:"subject_type"`
	SubjectID   string     `db:"subject_id"`
	AuthorID    string     `db:"author_id"`
	AuthorName  string     `db:"author_name"`
	Score       int        `db:"score"`
	Body        string     `db:"body"`
	Status      string     `db:"status"`
	UnderReview int64      `db:"under_review"`
	EditedAt    *time.Time `db:"edited_at"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}

// ratingTable is the ratings table, Global: no tenant filter on any statement.
// The name is written here rather than imported, because the package that owns
// the constant imports this one.
var ratingTable = model.NewTable(model.TableSpec{
	Name:         "ratings",
	New:          func() model.Entity { return new(Rating) },
	ManualKey:    true,
	NoTimestamps: true,
	Global:       true,
})

// RatingColumns are the columns Rating reads.
var RatingColumns = []any{
	"id", "tenant_id", "subject_type", "subject_id", "author_id", "author_name", "score", "body", "status",
	"under_review", "edited_at", "created_at", "updated_at",
}

// Report is a row of the reports table, of any tenant.
type Report struct {
	model.Model

	ID            string     `db:"id"`
	TenantID      string     `db:"tenant_id"`
	RatingID      string     `db:"rating_id"`
	RatingTenant  string     `db:"rating_tenant"`
	SubjectType   string     `db:"subject_type"`
	SubjectID     string     `db:"subject_id"`
	ReporterID    string     `db:"reporter_id"`
	ReporterName  string     `db:"reporter_name"`
	Reason        string     `db:"reason"`
	Status        string     `db:"status"`
	Outcome       string     `db:"outcome"`
	DecidedBy     string     `db:"decided_by"`
	DecidedByName string     `db:"decided_by_name"`
	DecidedAt     *time.Time `db:"decided_at"`
	DecisionNote  string     `db:"decision_note"`
	ScoreBefore   int        `db:"score_before"`
	BodyBefore    string     `db:"body_before"`
	ScoreAfter    int        `db:"score_after"`
	BodyAfter     string     `db:"body_after"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
}

// reportTable is the reports table, Global.
var reportTable = model.NewTable(model.TableSpec{
	Name:         "rating_reports",
	New:          func() model.Entity { return new(Report) },
	ManualKey:    true,
	NoTimestamps: true,
	Global:       true,
})

// ReportColumns are the columns Report reads.
var ReportColumns = []any{
	"id", "tenant_id", "rating_id", "rating_tenant", "subject_type", "subject_id", "reporter_id", "reporter_name",
	"reason", "status", "outcome", "decided_by", "decided_by_name", "decided_at", "decision_note",
	"score_before", "body_before", "score_after", "body_after", "created_at", "updated_at",
}
