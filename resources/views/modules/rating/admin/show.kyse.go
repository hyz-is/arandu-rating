//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// ShowData is what the handler hands this page.
type ShowData = rating.TeamShowPageData

// beforeStars is the read-only group of stars of the rating as it was reported.
func beforeStars(data ShowData) components.RatingProps {
	return components.RatingProps{
		ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
		Name:           "score-before", Label: data.StarsLabel(data.Report.Subject, data.Report.ScoreBefore), Value: data.Labels.ScoreValue(data.Report.ScoreBefore),
		ReadOnly: true, OptionLabels: data.Labels.StarNames(),
	}
}

// nowStars is the read-only group of stars of the rating as it stands now.
func nowStars(data ShowData) components.RatingProps {
	return components.RatingProps{
		ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
		Name:           "score-now", Label: data.StarsLabel(data.Report.Subject, data.Rating.Score), Value: data.Rating.ScoreText,
		ReadOnly: true, OptionLabels: data.Labels.StarNames(),
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-6xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>

	<header class="mt-3 flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.team_show_title") }}</p>
			<h1 class="text-2xl font-semibold tracking-tight break-words">{{ .Report.Subject }}</h1>
		</div>
		<div class="flex flex-wrap gap-2">
			{!! components.Badge(components.BadgeProps{Label: .Report.Status, Variant: rating.ReportVariant(.Report.StatusKey)}) !!}
			{!! components.Badge(components.BadgeProps{Label: .Rating.Status, Variant: rating.StatusVariant(.Rating.StatusKey)}) !!}
		</div>
	</header>

	@if(.HasErrors())
		<ul class="text-destructive mt-4 grid gap-1 text-sm" role="alert">
			@foreach(.All() as line)
				<li>{{ line }}</li>
			@endforeach
		</ul>
	@endif

	<div class="mt-6 grid min-w-0 gap-6 lg:grid-cols-3">
		<div class="grid min-w-0 content-start gap-6 lg:col-span-2">
			<div class="card grid gap-3 p-5">
				<h2 class="text-base font-semibold">{{ .Labels.T("screen.reported_rating") }}</h2>
				<p class="text-sm font-semibold">{{ .Rating.Author }} <span class="text-muted-foreground font-normal">· {{ .Rating.Created }}</span></p>
				{!! components.Rating(beforeStars(.)) !!}
				@if(.Report.BodyBefore != "")
					<p class="text-sm break-words whitespace-pre-line">{{ .Report.BodyBefore }}</p>
				@endif
			</div>

			<div class="card grid gap-3 p-5">
				<h2 class="text-base font-semibold">{{ .Labels.T("screen.reason_title") }}</h2>
				<p class="text-muted-foreground text-sm">{{ .Report.Reporter }} · {{ .Report.Created }}</p>
				<p class="text-sm break-words whitespace-pre-line">{{ .Report.Reason }}</p>
			</div>

			@if(.Report.Decided != "")
				<div class="card grid gap-3 p-5">
					<h2 class="text-base font-semibold">{{ .Labels.T("screen.decision_title") }}</h2>
					<p class="text-muted-foreground text-sm">{{ .Report.DecidedBy }} · {{ .Report.Decided }}</p>
					@if(.Report.Outcome != "")
						<p class="text-sm font-semibold">{{ .Report.Outcome }}</p>
					@else
						<p class="text-sm font-semibold">{{ .Labels.T("screen.dismissed") }}</p>
					@endif
					<p class="text-sm break-words whitespace-pre-line">{{ .Report.DecisionNote }}</p>
					@if(.Report.OutcomeKey == "altered")
						<h3 class="text-sm font-semibold">{{ .Labels.T("screen.after") }}</h3>
						{!! components.Rating(nowStars(.)) !!}
						<p class="text-sm break-words whitespace-pre-line">{{ .Report.BodyAfter }}</p>
					@endif
				</div>
			@endif

			@if(.CanDecide)
				<form class="card grid gap-4 p-5" method="post" action="{{ .DecideURL }}">
					@csrf
					<h2 class="text-base font-semibold">{{ .Labels.T("screen.decide_title") }}</h2>
					<p class="text-muted-foreground -mt-2 text-sm">{{ .Labels.T("screen.decide_lead") }}</p>
					{!! components.Select(components.SelectProps{ID: "rating-verdict", Name: "verdict", Label: .Labels.T("field.verdict"), Placeholder: .Labels.T("control.choose"), Required: true, Options: .VerdictOptions, Page: .Form()}) !!}
					{!! components.Textarea(components.TextareaProps{ID: "rating-new-body", Name: "body", Label: .Labels.T("field.new_body"), Hint: .Labels.T("screen.new_body_hint"), Value: .Rating.Body, Rows: 5, Page: .Form()}) !!}
					@if(.ScoreEditable)
						{!! components.Select(components.SelectProps{ID: "rating-new-score", Name: "score", Label: .Labels.T("field.new_score"), Value: "", Options: .ScoreOptions, Page: .Form()}) !!}
					@endif
					{!! components.Textarea(components.TextareaProps{ID: "rating-note", Name: "note", Label: .Labels.T("field.note"), Hint: .Labels.T("screen.note_hint"), Rows: 4, Required: true, Page: .Form()}) !!}
					<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.decide"), Type: "submit"}) !!}</div>
				</form>
			@endif
		</div>

		<aside class="grid min-w-0 content-start gap-4" aria-label="{{ .Labels.T("screen.details") }}">
			<dl class="card grid gap-2 p-4 text-sm">
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.subject") }}</dt>
					<dd class="break-words">
						@if(.Subject.URL != "")
							<a class="hover:underline" href="{{ .Subject.URL }}">{{ .Subject.Name }}</a>
						@else
							{{ .Subject.Name }}
						@endif
					</dd></div>
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.author") }}</dt><dd class="break-words">{{ .Rating.Author }}</dd></div>
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.reporter") }}</dt><dd class="break-words">{{ .Report.Reporter }}</dd></div>
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.average") }}</dt><dd>{{ .Reading(.Subject) }}</dd></div>
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.reported_at") }}</dt><dd>{{ .Report.Created }}</dd></div>
			</dl>
		</aside>
	</div>
</section>
@endsection
