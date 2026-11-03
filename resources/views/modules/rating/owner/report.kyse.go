//go:build kyse

package owner

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// ReportData is what the handler hands this page.
type ReportData = rating.OwnerReportPageData

// scoreStars is the read-only group of stars that shows the reported rating.
func scoreStars(data ReportData) components.RatingProps {
	return components.RatingProps{
		ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
		Name:           "score-shown", Label: data.StarsLabel(data.Rating.Author, data.Rating.Score), Value: data.Rating.ScoreText, ReadOnly: true,
		OptionLabels: data.Labels.StarNames(),
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-2xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>

	<header class="mt-3">
		<p class="text-muted-foreground text-sm">{{ .Subject.Name }}</p>
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.owner_report_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.owner_report_lead") }}</p>
	</header>

	<div class="card mt-6 grid gap-3 p-5">
		<p class="text-sm font-semibold">{{ .Rating.Author }} <span class="text-muted-foreground font-normal">· {{ .Rating.Created }}</span></p>
		{!! components.Rating(scoreStars(.)) !!}
		@if(.Rating.Body != "")
			<p class="text-sm break-words whitespace-pre-line">{{ .Rating.Body }}</p>
		@endif
	</div>

	<form class="card mt-6 grid gap-4 p-5" method="post" action="{{ .Action }}">
		@csrf
		@if(.HasErrors())
			<ul class="text-destructive grid gap-1 text-sm" role="alert">
				@foreach(.All() as line)
					<li>{{ line }}</li>
				@endforeach
			</ul>
		@endif
		{!! components.Textarea(components.TextareaProps{Name: "reason", Label: .Labels.T("field.reason"), Hint: .Labels.T("screen.reason_hint"), Rows: 6, Required: true, Page: .Form()}) !!}
		<div class="flex flex-wrap items-center gap-2">
			{!! components.Button(components.ButtonProps{Label: .Labels.T("control.report"), Type: "submit"}) !!}
			<a class="btn" data-variant="outline" href="{{ .BackURL }}">{{ .Labels.T("control.cancel") }}</a>
		</div>
	</form>
</section>
@endsection
