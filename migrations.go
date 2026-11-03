package rating

import (
	"context"

	"github.com/arandu-io/framework/foundation"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// Migrations declares the schema this module owns.
//
// They are returned in the order their names sort in, which is the order they
// apply in: the name carries the order, and nothing else decides it.
func (m *Module) Migrations() []foundation.Migration {
	return []foundation.Migration{createRatings{}}
}

// The migration is reversible, and the assertion is here rather than discovered
// at rollback: the migrator tests for Down with a type assertion, so a Down
// with the wrong signature would leave a rollback that silently does nothing.
var _ migrations.ReversibleMigration = createRatings{}

// createRatings is the two tables this module owns, and the indexes its
// listings read by.
type createRatings struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order. It is fixed
// once the package is published: changing what an applied name means leaves the
// change missing everywhere it already ran, and nothing says so.
func (createRatings) GetName() string { return "20261103_0001_create_ratings" }

// Up creates the tables and the indexes.
//
// The Blueprint spells each column for the engine the migration is running on,
// which is what lets one application develop on a file and deploy on Postgres
// without a second schema. The timestamps have no database default: the value
// comes from Go, which is also what lets a test hold the clock.
func (createRatings) Up(ctx context.Context, conn migrations.Connection) error {
	if err := conn.Schema().Create(ctx, ratingsTable, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.String("subject_type", maxRefLen)
		table.String("subject_id", maxRefLen)
		table.String("author_id", maxIdentifierLen)
		table.String("author_name", maxNameLen).Default("")
		table.Integer("score")
		table.Text("body")
		table.String("status", 16)
		table.Integer("under_review").Default(0)
		table.Text("moderation_note")
		table.Timestamp("moderated_at").Nullable()
		table.String("moderated_by", maxIdentifierLen).Default("")
		table.Timestamp("edited_at").Nullable()
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		// An account rates a subject once: this is the net under the check the
		// service makes, and it is what refuses two writes at the same instant.
		table.Unique([]string{"tenant_id", "subject_type", "subject_id"}, "ratings_tenant_subject_unique")
		// The indexes match the ORDER BY of the listings, tenant first for the
		// author's and by subject for the owner's and the averages. Without them
		// every page is a scan of every customer's rows.
		table.Index([]string{"tenant_id", "updated_at", "id"}, "ratings_tenant_updated_idx")
		table.Index([]string{"tenant_id", "author_id", "updated_at"}, "ratings_tenant_author_idx")
		table.Index([]string{"subject_type", "subject_id", "status", "created_at"}, "ratings_subject_idx")
	}); err != nil {
		return err
	}

	return conn.Schema().Create(ctx, reportsTable, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.String("rating_id")
		table.String("rating_tenant")
		table.String("subject_type", maxRefLen)
		table.String("subject_id", maxRefLen)
		table.String("reporter_id", maxIdentifierLen)
		table.String("reporter_name", maxNameLen).Default("")
		table.Text("reason")
		table.String("status", 16)
		table.String("outcome", 16).Default("")
		table.String("decided_by", maxIdentifierLen).Default("")
		table.String("decided_by_name", maxNameLen).Default("")
		table.Timestamp("decided_at").Nullable()
		table.Text("decision_note")
		table.Integer("score_before")
		table.Text("body_before")
		table.Integer("score_after").Default(0)
		table.Text("body_after")
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		// The owner follows their own reports, newest first.
		table.Index([]string{"tenant_id", "created_at", "id"}, "rating_reports_tenant_idx")
		// The queue: by status, oldest waiting first.
		table.Index([]string{"status", "created_at", "id"}, "rating_reports_queue_idx")
		// What is known about one rating.
		table.Index([]string{"rating_id", "status"}, "rating_reports_rating_idx")
	})
}

// Down drops the tables, which takes their indexes with them.
func (createRatings) Down(ctx context.Context, conn migrations.Connection) error {
	for _, table := range []string{reportsTable, ratingsTable} {
		if err := conn.Schema().DropIfExists(ctx, table); err != nil {
			return err
		}
	}
	return nil
}
