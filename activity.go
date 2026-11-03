package rating

import (
	"context"
	"strings"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/translation"
	activitylog "github.com/hyz-is/arandu-activitylog"
)

// ActivityLogName is the log every entry of this package is recorded in.
const ActivityLogName = "rating"

// ActivityRefType is the kind of subject an entry about a rating names.
const ActivityRefType = "rating"

// The events this package records, one per act that changes something. The
// subject of each entry is the rating.
//
// No entry carries the text of a rating, of a report or of a decision: that an
// act happened, who did it and what it changed are what the log keeps, and what
// was said stays where the side that may read it reads it.
const (
	ActivityRatingCreated    = "rating.created"
	ActivityRatingUpdated    = "rating.updated"
	ActivityRatingWithdrawn  = "rating.withdrawn"
	ActivityRatingReported   = "rating.reported"
	ActivityRatingRemoved    = "rating.removed"
	ActivityRatingAltered    = "rating.altered"
	ActivityReportDismissed  = "rating.report_dismissed"
	activityDescriptionGroup = "activity."
)

// record writes one entry of the act just done, under the Grant it was
// authorized with: the tenant of the entry is the act's, data.Tenant(g), and
// the causer is the subject who acted, named explicitly because a moderator's
// act is written under a Grant scoped to a customer's tenant, whose own subject
// is nobody. Without Config.Activity it records nothing.
//
// The act already happened when this runs, so a failure to record it is
// returned to the caller rather than swallowed: an act the log does not show is
// the one an audit asks about.
func (c *core) record(ctx context.Context, g security.Grant, actor security.Subject, event, ratingID string, subject Ref, properties map[string]any) error {
	if c.cfg.Activity == nil {
		return nil
	}
	description := c.labels.line(activityDescriptionGroup+strings.ReplaceAll(event, ".", "_"), translation.Replace{"subject": subject.String()})
	entry := c.cfg.Activity.In(ctx, g, ActivityLogName).Event(event).
		OnRef(activitylog.Ref{Type: ActivityRefType, ID: ratingID})
	if actor.ID != "" {
		entry = entry.ByRef(activitylog.Ref{Type: "user", ID: actor.ID})
	}
	props := map[string]any{"subject_type": subject.Type, "subject_id": subject.ID}
	for key, value := range properties {
		props[key] = value
	}
	_, err := entry.WithProperties(props).Log(description)
	return err
}

// notify tells the application's Notifier what happened. It runs after the
// change is stored and its failure is logged, not returned: see Notifier.
func (c *core) notify(ctx context.Context, e Event) {
	if c.cfg.Notifier == nil {
		return
	}
	if err := c.cfg.Notifier.Notify(ctx, e); err != nil {
		c.cfg.Logger.Warn("rating: the notifier failed after the change was stored", "event", string(e.Kind), "rating", e.Rating.ID, "error", err)
	}
}
