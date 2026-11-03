package feature_test

import (
	"reflect"
	"testing"

	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

// The data table of kyse says twenty-nine sentences and says the English under
// every one the screen leaves empty, so a screen that forgot one is a table in
// two languages. Every field of DataTableLines is a line of the catalogue, in
// each locale the package ships.
func TestTheDataTableSaysEverySentenceInTheScreensLanguage(t *testing.T) {
	t.Parallel()
	for _, locale := range rating.Locales() {
		w := installWith(t, func(c *rating.Config) { c.Locale = locale })
		lines := rating.Screen{Labels: w.module.Labels()}.TableLines()
		value := reflect.ValueOf(lines)
		for i := 0; i < value.NumField(); i++ {
			// Rows is the one sentence for every count, and a sentence given there wins
			// over the singular and the plural: the screens leave it empty and say
			// RowsOne and RowsMany.
			if value.Type().Field(i).Name == "Rows" {
				continue
			}
			if value.Field(i).String() == "" {
				t.Errorf("%s: the table says nothing for %s", locale, value.Type().Field(i).Name)
			}
		}
	}
}

// The state a list hands the table carries what the request asked for, so every
// control of the table keeps it: the search, the facets, the order and the
// page.
func TestTheListsHandTheTableTheStateOfTheRequest(t *testing.T) {
	t.Parallel()
	order := rating.TableOrder{
		Query: "câmera", SortKey: "score", SortDir: "asc", CurrentPage: 2, PerPage: 25,
		Facets: map[string][]string{"status": {"published"}, "score": {"4", "5"}},
	}
	values := order.State().Values(components.DataTableChange{})
	if values.Get("q") != "câmera" || values.Get("status") != "published" || values.Get("page") != "2" ||
		values.Get("sort") != "score" || values.Get("dir") != "asc" || len(values["score"]) != 2 {
		t.Fatalf("the state is %v", values)
	}
}
