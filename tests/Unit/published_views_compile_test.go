package unit_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/framework/foundation"
)

// viewCompiler is the command line whose view compiler the published views are
// held against, at the release an application builds them with.
//
// It is a version and never "latest", so this test changes its answer when this
// line changes and never because somebody published something. Moving it is
// how a stricter compiler is taken on: raise it, run the suite, and fix the
// view it refuses here rather than in every application that publishes it.
const viewCompiler = "github.com/arandu-io/aru@v0.74.0"

// TestEveryPublishedViewCompiles publishes the views into a project of their
// own, builds them with the view compiler an application runs, and compiles
// what it wrote.
//
// Nothing else in this repository reads the markup. The sources open with a
// build tag, so go build, go vet and go test never look past it, and the view
// compiler refuses what the Go compiler would accept -- a value written into an
// address behind text it cannot read, a value in a position no escape covers.
// A view it refuses is a view that stops the build of every application that
// publishes it, and without this test the first place to say so is somebody
// else's terminal.
//
// The project is laid out as an application's is once it has published: each
// file where the publication writes it, and a go.mod that resolves this package
// to the checkout under test. What the view compiler writes then goes through
// the Go compiler, which is what catches a field the markup names and the page
// data does not have.
//
// It does not skip. A gate that steps aside when its input is missing is how
// the views stopped compiling with nothing here saying so.
func TestEveryPublishedViewCompiles(t *testing.T) {
	root := packageRoot(t)
	project := t.TempDir()

	publications, err := foundation.Publications(module(t))
	if err != nil {
		t.Fatalf("reading what the module publishes: %v", err)
	}
	var published int
	for _, publication := range publications {
		from := publication.From
		if from == "" {
			from = "."
		}
		err := fs.WalkDir(publication.Files, from, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative := name
			if from != "." {
				relative = strings.TrimPrefix(strings.TrimPrefix(name, from), "/")
			}
			body, err := fs.ReadFile(publication.Files, name)
			if err != nil {
				return err
			}
			target := filepath.Join(project, filepath.FromSlash(path.Join(publication.To, relative)))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			published++
			return os.WriteFile(target, body, 0o644)
		})
		if err != nil {
			t.Fatalf("publishing the %s files: %v", publication.Tag, err)
		}
	}
	if published == 0 {
		t.Fatal("the module publishes no file, so this gate would compile nothing")
	}

	// The application's go.mod names this package and points it at the
	// checkout, so the views compile against the code beside them rather than
	// against a release. The sums come from here: the application needs no
	// module this package does not already require.
	goMod := readReleaseFile(t, root, "go.mod")
	language := captureReleaseValue(t, goMod, `(?m)^go ([0-9.]+)$`, "go directive in go.mod")
	modulePath := captureReleaseValue(t, goMod, `(?m)^module (\S+)$`, "module path in go.mod")
	manifest := "module example.com/application\n\n" +
		"go " + language + "\n\n" +
		"require " + modulePath + " v0.0.0-00010101000000-000000000000\n\n" +
		"replace " + modulePath + " => " + strconv.Quote(root) + "\n"
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.sum"), []byte(readReleaseFile(t, root, "go.sum")), 0o644); err != nil {
		t.Fatal(err)
	}

	inProject(t, project, "the published views do not compile with "+viewCompiler+", so no application that publishes them can build",
		"go", "run", viewCompiler, "view:build")
	inProject(t, project, "the views "+viewCompiler+" compiled do not build against this package",
		"go", "build", "./...")

	// Compiling is not drawing. Each screen is rendered once, with a row of
	// every kind and every action switched on, under a layout that draws the
	// content section alone -- so a page that compiles and then fails on its
	// own data, or draws a key instead of a sentence, fails here.
	probe := filepath.Join(project, "renderprobe")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probe, "main.go"), []byte(renderProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	inProject(t, project, "a published view does not render the page data its handler hands it",
		"go", "run", "./renderprobe")
}

// renderProbe renders every screen with sample data and checks what it drew.
const renderProbe = `package main

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"

	"github.com/arandu-io/hesape/view"
	"github.com/arandu-io/kyse/components"
	rating "github.com/hyz-is/arandu-rating"

	_ "example.com/application/storage/framework/views/modules/rating"
	_ "example.com/application/storage/framework/views/modules/rating/admin"
	_ "example.com/application/storage/framework/views/modules/rating/owner"
)

func main() {
	view.RegisterLayout("layouts.app", func(w io.Writer, _ any, sections map[string]func(io.Writer) error) error {
		return sections["content"](w)
	})
	sc := rating.Screen{
		Labels: rating.Labels{Locale: "pt-BR"},
		URLs:   rating.NewURLs("/painel/avaliacoes", "/painel/avaliacoes-recebidas", "/admin/avaliacoes", rating.PortuguesePaths),
	}
	subject := rating.SubjectRow{Name: "Câmera Norte", URL: "/catalogo/42", Key: "application/42", Count: 12, Average: "4,3", Stars: 4, StarsValue: "4", Rated: true,
		OwnURL: "/painel/avaliacoes-recebidas/assunto/application/42"}
	row := rating.RatingRow{ID: "r1", Subject: "Câmera Norte", SubjectURL: "/catalogo/42", SubjectKey: "application/42", Author: "Ana Souza",
		Score: 4, ScoreText: "4", Body: "Funciona bem, <b>mas</b> trava às vezes.", Status: "Publicada", StatusKey: "published",
		Created: "03/11/2026 12:00", Updated: "03/11/2026 13:30", CreatedSort: "2026-11-03T15:00:00Z", UpdatedSort: "2026-11-03T16:30:00Z",
		URL: "/painel/avaliacoes/r1", ReportURL: "/painel/avaliacoes-recebidas/denuncias/r1"}
	reviewed := row
	reviewed.UnderReview, reviewed.ReportURL = true, ""
	removed := row
	removed.Status, removed.StatusKey, removed.ModerationNote, removed.Moderated = "Removida pela moderação", "removed", "Ofensa ao autor do aplicativo.", "04/11/2026 09:00"
	report := rating.ReportRow{ID: "p1", RatingID: "r1", Subject: "Câmera Norte", SubjectURL: "/catalogo/42", SubjectKey: "application/42",
		Reporter: "Gil Lopes", Reason: "A conta nunca usou o aplicativo.", Status: "Aberta", StatusKey: "open", Created: "03/11/2026 14:00",
		CreatedSort: "2026-11-03T17:00:00Z", URL: "/admin/avaliacoes/p1", Open: true, ScoreBefore: 4, BodyBefore: "Funciona bem, mas trava às vezes."}
	decided := report
	decided.Status, decided.StatusKey, decided.Open = "Acatada", "upheld", false
	decided.Outcome, decided.OutcomeKey, decided.DecidedBy, decided.Decided = "Texto corrigido", "altered", "Rita", "04/11/2026 09:00"
	decided.DecisionNote, decided.ScoreAfter, decided.BodyAfter = "Retirado o trecho ofensivo.", 4, "Funciona bem."
	options := func(values ...string) []components.SelectOption {
		out := make([]components.SelectOption, 0, len(values))
		for _, v := range values {
			out = append(out, components.SelectOption{Value: v, Label: v})
		}
		return out
	}
	order := rating.TableOrder{SortKey: "updated_at", SortDir: "desc", CurrentPage: 1, PerPage: 25}

	pages := []struct {
		name string
		data any
		want []string
		not  []string
	}{
		{rating.ViewIndex, rating.IndexPageData{Screen: sc, TableOrder: order, Rows: []rating.RatingRow{row, reviewed}, URL: "/painel/avaliacoes",
			Pages: 1, Total: 2, Pager: components.Paginator(nil), StatusOptions: options("published"), ScoreOptions: options("5")},
			[]string{"Minhas avaliações", "Câmera Norte", "/painel/avaliacoes/r1", "Publicada", "Em análise", "Buscar em Minhas avaliações", "Situação", "Nota",
				"4 de 5 estrelas", "type=\"radio\"", "disabled"},
			[]string{"<b>", "Ana Souza"}},
		{rating.ViewIndex, rating.IndexPageData{Screen: sc, TableOrder: order, URL: "/painel/avaliacoes", Pages: 1},
			[]string{"Nenhuma avaliação por aqui", "Quando a sua conta avaliar algo"}, nil},
		{rating.ViewForm, rating.FormPageData{Screen: sc, Subject: subject, Action: "/painel/avaliacoes/assunto/application/42", BackURL: "/painel/avaliacoes",
			MaxChars: 2000},
			[]string{"Avaliar", "Câmera Norte", "/catalogo/42", "Nota", "Comentário", "name=\"score\"", "required", "Excelente", "Péssimo", "Opcional. Até 2000 caracteres.",
				"Publicar avaliação", "Cancelar", "_token", "Média 4,3 em 12 avaliações"}, nil},
		{rating.ViewForm, rating.FormPageData{Screen: sc, Subject: rating.SubjectRow{Name: "Plano Pro", Key: "plan/9"}, Action: "/x", BackURL: "/y", MaxChars: 500, BodyRequire: true},
			[]string{"Plano Pro", "Até 500 caracteres"}, []string{"Média", "Opcional"}},
		{rating.ViewShow, rating.ShowPageData{Screen: sc, Rating: row, Subject: subject, BackURL: "/painel/avaliacoes",
			UpdateURL: "/painel/avaliacoes/r1/edicao", WithdrawURL: "/painel/avaliacoes/r1/retirada", CanEdit: true, CanWithdraw: true, MaxChars: 2000},
			[]string{"Câmera Norte", "Publicada", "Funciona bem, &lt;b&gt;mas&lt;/b&gt; trava às vezes.", "Alterar avaliação", "/painel/avaliacoes/r1/edicao",
				"/painel/avaliacoes/r1/retirada", "Apagar avaliação", "withdraw-rating", "_token"},
			[]string{"<b>mas</b>", "Motivo registrado pela moderação"}},
		{rating.ViewShow, rating.ShowPageData{Screen: sc, Rating: reviewed, Subject: subject, BackURL: "/painel/avaliacoes"},
			[]string{"Em análise", "não pode ser alterada nem apagada"}, []string{"Alterar avaliação", "Apagar avaliação"}},
		{rating.ViewShow, rating.ShowPageData{Screen: sc, Rating: removed, Subject: subject, BackURL: "/painel/avaliacoes"},
			[]string{"Removida pela moderação", "Motivo registrado pela moderação", "Ofensa ao autor do aplicativo.", "Decisão da moderação em"},
			[]string{"Alterar avaliação", "Apagar avaliação"}},
		{rating.ViewOwnerIndex, rating.OwnerIndexPageData{Screen: sc, TableOrder: rating.TableOrder{SortKey: "name", SortDir: "asc", CurrentPage: 1, PerPage: 25},
			Rows: []rating.SubjectRow{subject, {Name: "Plano Pro", Key: "plan/9", OwnURL: "/painel/avaliacoes-recebidas/assunto/plan/9"}}, URL: "/painel/avaliacoes-recebidas",
			Pages: 1, Total: 2, Pager: components.Paginator(nil)},
			[]string{"Avaliações recebidas", "Câmera Norte", "/painel/avaliacoes-recebidas/assunto/application/42", "4,3", "12 avaliações", "Sem avaliações",
				"Buscar em Avaliações recebidas", "Denúncias enviadas", "/painel/avaliacoes-recebidas/denuncias"}, nil},
		{rating.ViewOwnerSubject, rating.OwnerSubjectPageData{Screen: sc, TableOrder: order, Subject: subject, Rows: []rating.RatingRow{row, reviewed},
			URL: "/painel/avaliacoes-recebidas/assunto/application/42", BackURL: "/painel/avaliacoes-recebidas", Pages: 1, Total: 2,
			Pager: components.Paginator(nil), ScoreOptions: options("5")},
			[]string{"Câmera Norte", "Ana Souza", "Funciona bem, &lt;b&gt;mas&lt;/b&gt; trava às vezes.", "Denunciar", "/painel/avaliacoes-recebidas/denuncias/r1",
				"Em análise", "Média 4,3 em 12 avaliações", "Nota"},
			[]string{"<b>mas</b>"}},
		{rating.ViewOwnerReports, rating.OwnerReportsPageData{Screen: sc, TableOrder: order, Rows: []rating.ReportRow{report, decided},
			URL: "/painel/avaliacoes-recebidas/denuncias", BackURL: "/painel/avaliacoes-recebidas", Pages: 1, Total: 2, Pager: components.Paginator(nil),
			StatusOptions: options("open")},
			[]string{"Denúncias enviadas", "A conta nunca usou o aplicativo.", "Aberta", "Acatada", "Texto corrigido", "Retirado o trecho ofensivo."}, []string{"Rita"}},
		{rating.ViewOwnerReport, rating.OwnerReportPageData{Screen: sc, Rating: row, Subject: subject, Action: "/painel/avaliacoes-recebidas/denuncias/r1",
			BackURL: "/painel/avaliacoes-recebidas/assunto/application/42", MaxChars: 1000},
			[]string{"Denunciar avaliação", "Ana Souza", "Funciona bem", "Motivo", "name=\"reason\"", "required", "/painel/avaliacoes-recebidas/denuncias/r1", "_token"}, nil},
		{rating.ViewTeamIndex, rating.TeamIndexPageData{Screen: sc, TableOrder: rating.TableOrder{SortKey: "created_at", SortDir: "asc", CurrentPage: 1, PerPage: 25},
			Rows: []rating.ReportRow{report}, URL: "/admin/avaliacoes", Pages: 3, Total: 60, Pager: components.Paginator(nil), StatusOptions: options("open")},
			[]string{"Denúncias de avaliações", "Câmera Norte", "Gil Lopes", "A conta nunca usou o aplicativo.", "Aberta", "/admin/avaliacoes/p1",
				"Buscar por motivo, denunciante ou assunto"}, nil},
		{rating.ViewTeamShow, rating.TeamShowPageData{Screen: sc, Report: report, Rating: row, Subject: subject, BackURL: "/admin/avaliacoes",
			DecideURL: "/admin/avaliacoes/p1/decisao", CanDecide: true, ScoreEditable: true, VerdictOptions: options("remove", "alter", "dismiss"),
			ScoreOptions: options("", "5"), MaxBody: 2000, MaxNote: 1000},
			[]string{"Câmera Norte", "Ana Souza", "Gil Lopes", "A conta nunca usou o aplicativo.", "Funciona bem, mas trava às vezes.", "Decidir",
				"/admin/avaliacoes/p1/decisao", "name=\"verdict\"", "name=\"body\"", "name=\"note\"", "name=\"score\"", "Registrar decisão", "_token"},
			[]string{"Decisão</h2>"}},
		{rating.ViewTeamShow, rating.TeamShowPageData{Screen: sc, Report: decided, Rating: row, Subject: subject, BackURL: "/admin/avaliacoes"},
			[]string{"Decisão", "Rita", "Texto corrigido", "Retirado o trecho ofensivo.", "Como ficou", "Funciona bem."},
			[]string{"Registrar decisão", "name=\"verdict\""}},
	}

	inline := regexp.MustCompile(` + "`" + `(?i)(<script|\sstyle=|\son[a-z]+=|javascript:)` + "`" + `)
	failed := false
	for _, p := range pages {
		rec := httptest.NewRecorder()
		if err := view.NewRenderer().Render(context.Background(), rec, 200, p.name, p.data); err != nil {
			fmt.Printf("%s: %v\n", p.name, err)
			failed = true
			continue
		}
		body := rec.Body.String()
		for _, want := range p.want {
			if !strings.Contains(body, want) {
				fmt.Printf("%s: the page does not say %q\n", p.name, want)
				failed = true
			}
		}
		for _, not := range p.not {
			if strings.Contains(body, not) {
				fmt.Printf("%s: the page says %q, which this data must not draw\n", p.name, not)
				failed = true
			}
		}
		if strings.Contains(body, "rating.screen.") || strings.Contains(body, "rating.field.") || strings.Contains(body, "rating.control.") {
			fmt.Printf("%s: the page draws a catalogue key instead of its sentence\n", p.name)
			failed = true
		}
		// The content security policy is 'self': no inline script, no inline
		// style, no event attribute and no javascript: address.
		if found := inline.FindString(body); found != "" {
			fmt.Printf("%s: the page carries %q, which a policy of 'self' refuses\n", p.name, found)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
`

// inProject runs one command in the scratch project, outside any workspace,
// and fails with what it printed.
//
// -mod=mod lets the go command fill in the requirements the application's
// go.mod leaves to this package's, which is what an application's own `go mod
// tidy` would have written.
func inProject(t *testing.T, dir, failure string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS="+strings.TrimSpace(os.Getenv("GOFLAGS")+" -mod=mod"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s:\n$ %s %s\n%s", failure, name, strings.Join(args, " "), output)
	}
}
