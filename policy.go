package rating

import (
	"context"
	"fmt"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"
)

// The actions of this package. Constants rather than strings at the call site:
// a typo in an action name would silently authorize nothing, or worse,
// everything.
//
// They carry the entity in the name because an application registers many
// packages, and the name of an action shows up in logs and in audit trails
// where "view" on its own says nothing about what was viewed.
//
// The actions are what an arandu-permission group carries: the application
// declares them in the catalogue (Actions and TeamActions), an organization or
// the platform grants them to whom it decides, and arandu-permission fills the
// subject's actions on every request. Nothing here is decided by a role.
const (
	// RatingCreate is rating a subject in the subject's own space.
	RatingCreate security.Action = "rating.create"
	// RatingViewOwn is reading the ratings the subject wrote.
	RatingViewOwn security.Action = "rating.view_own"
	// RatingViewSpace is reading every rating of the subject's space, whoever
	// wrote it.
	RatingViewSpace security.Action = "rating.view_space"
	// RatingUpdate is changing a rating the subject wrote.
	RatingUpdate security.Action = "rating.update"
	// RatingDelete is deleting a rating the subject wrote.
	RatingDelete security.Action = "rating.delete"
	// RatingManageSpace is changing and deleting the space's ratings whoever
	// wrote them, with RatingUpdate or RatingDelete.
	RatingManageSpace security.Action = "rating.manage_space"
	// RatingViewSummary is reading what the ratings of a subject add up to: how
	// many, and the average. It opens no text and no name.
	RatingViewSummary security.Action = "rating.view_summary"

	// RatingViewSubject is the owner of a subject reading the ratings it
	// received, and the reports the owner's space filed.
	RatingViewSubject security.Action = "rating.view_subject"
	// RatingReport is the owner of a subject reporting a rating of it.
	RatingReport security.Action = "rating.report"

	// RatingViewAll is the moderation reading every report of every space.
	RatingViewAll security.Action = "rating.view_all"
	// RatingModerate is the moderation deciding a report: removing a rating,
	// altering it or archiving the report.
	RatingModerate security.Action = "rating.moderate"
)

// Actions are the customer's actions, for an application to declare in
// arandu-permission's catalogue beside its own, so an organization grants them
// to whom it decides. They cover both customer sides: rating a subject, and
// being the owner of one.
func Actions() []security.Action {
	return []security.Action{
		RatingCreate, RatingViewOwn, RatingViewSpace, RatingUpdate, RatingDelete, RatingManageSpace, RatingViewSummary,
		RatingViewSubject, RatingReport,
	}
}

// TeamActions are the moderation's actions. They are declared apart from
// Actions so a catalogue an organization grants from never holds them: an
// application gives them to the groups of its own team only, and
// ModerationPolicy refuses them to any subject outside Config.TeamTenant
// besides.
func TeamActions() []security.Action {
	return []security.Action{RatingViewAll, RatingModerate}
}

// reads says an action only looks. Everything else is told to write, so an
// action added without a decision here fails the first time it tries to write
// under a name that says it only looks, and not the other way round.
func reads(a security.Action) bool {
	switch a {
	case RatingViewOwn, RatingViewSpace, RatingViewSummary, RatingViewSubject, RatingViewAll:
		return true
	}
	return false
}

// RatingPolicy is the only authority over what an account does with its own
// ratings.
//
// It denies unless a rule below says otherwise, and every rule reads what the
// subject was granted: a subject with no actions, an administrator included, is
// refused everything. Authorship decides the rest -- an author edits and
// deletes the rating they wrote, and the whole space's only with
// RatingManageSpace -- and the tenant of the record is checked first, before
// any rule, so a rule that opens an action opens it inside one space only.
type RatingPolicy struct{}

// Compile-time proof that the policy answers about this entity and no other. A
// policy that drifted onto another type would leave this one unguarded while
// the Model path still compiled.
var _ security.Policy[Rating] = RatingPolicy{}

// Compile-time proof that the policy says which of its actions write: without
// it the Grant issued to read a rating would also be one that writes.
var _ auth.Writes = RatingPolicy{}

// Writes reports whether a Grant issued for the action may change anything.
func (RatingPolicy) Writes(a security.Action) bool { return !reads(a) }

// Can decides whether the subject may perform the action on the record.
//
// It is the only place that decides about the author's side. The service
// reaches the Model only after Authorize turns this method's nil result into a
// Grant.
//
// The record is the empty Rating where the question is "may this subject do
// this kind of thing at all", and the loaded row where it is "may they do it to
// this rating". Both are asked, in that order, and the second is what a rule
// about authorship answers.
func (RatingPolicy) Can(_ context.Context, s security.Subject, a security.Action, record Rating) error {
	// Tenant isolation comes first and applies to every action. Without it every
	// check below would be pointless in a multi-tenant system: a rule that
	// allows an author to read their own record would allow it across customers
	// as soon as two of them have a record with the same identifier.
	//
	// The empty id is the candidate that has not been stored yet, which belongs
	// to nobody until it is written with the tenant off the Grant.
	if record.ID != "" && record.TenantID != s.Tenant {
		return fmt.Errorf("rating belongs to another tenant")
	}

	// arandu:begin custom
	if s.IsGuest() {
		return fmt.Errorf("a guest has no ratings")
	}
	if !s.Can(a) {
		return fmt.Errorf("the subject was not granted %s", a)
	}
	switch a {
	case RatingCreate:
		if record.ID == "" {
			return nil
		}
	case RatingViewSummary, RatingViewSpace:
		return nil
	case RatingViewOwn:
		// The probe -- the empty record -- asks whether this kind of reading is
		// possible at all, and the service then narrows the statement to the
		// subject's own ratings. The second call, on the loaded row, is where
		// authorship is decided.
		if record.ID == "" || record.AuthorID == s.ID {
			return nil
		}
	case RatingUpdate, RatingDelete:
		if record.ID == "" || record.AuthorID == s.ID || s.Can(RatingManageSpace) {
			return nil
		}
	}
	// arandu:end custom

	return fmt.Errorf("no rule allows %s on rating", a)
}

// Target is a subject as the owner's policy is asked about it: what it is, and
// the tenant the application says owns it. The zero Target is the probe -- "may
// this kind of thing be done at all".
type Target struct {
	Subject Ref
	// Owner is the tenant that owns the subject, as Subjects answered. It is
	// never a value from a request.
	Owner string
}

// OwnerPolicy is the only authority over what the owner of a subject does with
// the ratings it received.
//
// Ownership is the one thing the package cannot know, so the application
// answers it and the policy compares: a Target whose owner is not the subject's
// tenant is refused whatever the subject was granted, and the check comes
// before any rule.
type OwnerPolicy struct{}

var _ security.Policy[Target] = OwnerPolicy{}

var _ auth.Writes = OwnerPolicy{}

// Writes reports whether a Grant issued for the action may change anything.
func (OwnerPolicy) Writes(a security.Action) bool { return !reads(a) }

// Can decides whether the subject may perform the action on the target.
func (OwnerPolicy) Can(_ context.Context, s security.Subject, a security.Action, target Target) error {
	if !target.Subject.IsZero() && (target.Owner == "" || target.Owner != s.Tenant) {
		return fmt.Errorf("subject belongs to another tenant")
	}

	// arandu:begin custom
	if s.IsGuest() {
		return fmt.Errorf("a guest owns no subject")
	}
	if !s.Can(a) {
		return fmt.Errorf("the subject was not granted %s", a)
	}
	switch a {
	case RatingViewSubject, RatingReport:
		return nil
	}
	// arandu:end custom

	return fmt.Errorf("no rule allows %s on a subject", a)
}

// ModerationPolicy is the only authority over what the platform team does with
// the reports of every space.
//
// It denies unless the subject is a member of the team -- a subject of Realm,
// the tenant the team signs in to -- who was granted the action. A subject of
// any other tenant is refused whatever actions it carries, and so is a subject
// somebody else is viewing as: an operator inside a customer's account acts as
// the customer and reads no other space.
//
// It is the one policy that does not compare a record's tenant with the
// subject's, because crossing tenants is what the team does. The tenant of the
// work itself is not the subject's: the service reads the report to find it,
// and then acts in that tenant under a Grant scoped to it, issued only after
// this policy allowed the action.
type ModerationPolicy struct {
	// Realm is the tenant the team signs in to. Empty refuses everybody.
	Realm string
}

var _ security.Policy[Report] = ModerationPolicy{}

var _ auth.Writes = ModerationPolicy{}

// Writes reports whether a Grant issued for the action may change anything.
func (ModerationPolicy) Writes(a security.Action) bool { return !reads(a) }

// Can decides whether the subject may perform the action.
func (p ModerationPolicy) Can(_ context.Context, s security.Subject, a security.Action, _ Report) error {
	if p.Realm == "" || s.Tenant != p.Realm {
		return fmt.Errorf("only the platform team acts on every space's reports")
	}

	// arandu:begin custom
	if s.IsGuest() {
		return fmt.Errorf("a guest is no member of the team")
	}
	if s.Impersonator != nil {
		return fmt.Errorf("a subject inside somebody else's account reads no other space")
	}
	switch a {
	case RatingViewAll, RatingModerate:
		if s.Can(a) {
			return nil
		}
		return fmt.Errorf("the subject was not granted %s", a)
	}
	// arandu:end custom

	return fmt.Errorf("no rule allows %s on a report", a)
}
