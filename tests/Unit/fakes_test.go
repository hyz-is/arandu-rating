package unit_test

import (
	"context"

	rating "github.com/hyz-is/arandu-rating"
)

// catalogue is the application's Subjects for tests that only need the module
// to be built: it knows nothing.
type catalogue struct{}

func (catalogue) Subject(context.Context, rating.Ref) (rating.Subject, error) {
	return rating.Subject{}, rating.ErrNoSubject
}

func (catalogue) Owned(context.Context, string) ([]rating.Subject, error) { return nil, nil }

// everybody is the application's Eligibility when the answer is yes.
type everybody struct{}

func (everybody) CanRate(context.Context, string, string, rating.Ref) (bool, error) { return true, nil }

// wired is the smallest configuration the package accepts.
func wired() rating.Config {
	return rating.Config{TeamTenant: "platform", Subjects: catalogue{}, Eligibility: everybody{}}
}
