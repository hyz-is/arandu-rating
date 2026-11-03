package rating

import (
	"fmt"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"
)

// This file is the only place in the package that builds a Grant without a
// Policy answering for it, and the audit in tests/Unit holds that.
//
// A rating lives in the tenant of the account that wrote it, a report in the
// tenant of the owner of the subject, and the people who read or change them
// are in a third: the owner's space reports a rating of somebody else's, and
// the platform team decides a report in two spaces at once. A statement needs
// the Grant of the tenant it reads or writes, so scopedGrant narrows the Grant
// a Policy issued to the one tenant a row is in -- and only for the action that
// Grant was issued for.

// scopedGrant scopes a Grant a Policy issued to the tenant of one row.
//
// The Grant must have been issued for the action asked, so a Grant issued to
// read cannot be scoped into one that writes something else. When the act
// writes, the Grant it comes from has to be writable: a subject somebody is only
// viewing as can look at a rating and cannot touch the account next to it
// through this door. A subject somebody acts as keeps the one who is really
// acting, so the log of what the scoped Grant writes can name them.
func scopedGrant(from security.Grant, a security.Action, tenant string, write bool) (security.Grant, error) {
	if err := from.Check(a); err != nil {
		return security.Grant{}, err
	}
	if !security.ValidTenant(tenant) {
		return security.Grant{}, fmt.Errorf("rating: a row is in the tenant %q, which cannot be one", tenant)
	}
	if write {
		if err := auth.Writable(from); err != nil {
			return security.Grant{}, err
		}
	}
	if actor := from.Impersonator(); actor != nil && actor.ID != "" {
		//arandu:system-grant a Policy allowed the act for the subject somebody is acting as; it is scoped to the one tenant the row is in, naming who really acts
		return auth.SystemGrantActing(a, tenant, actor.ID), nil
	}
	//arandu:system-grant a Policy allowed the act; each read or write is scoped to the one tenant the row is in
	return security.SystemGrant(a, tenant), nil
}
