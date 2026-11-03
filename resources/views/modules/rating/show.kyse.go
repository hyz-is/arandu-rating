//go:build kyse

package rating

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// ShowData is what the handler hands this page.
type ShowData = rating.ShowPageData

// scoreStars is the read-only group of stars that shows the rating's score.
func scoreStars(data ShowData) components.RatingProps {
	return components.RatingProps{
		ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
		Name:           "score-shown", Label: data.StarsLabel(data.Rating.Subject, data.Rating.Score), Value: data.Rating.ScoreText, ReadOnly: true,
		OptionLabels: data.Labels.StarNames(),
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-2xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>

	<header class="mt-3 flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.show_title") }}</p>
			<h1 class="text-2xl font-semibold tracking-tight break-words">{{ .Rating.Subject }}</h1>
		</div>
		<div class="flex flex-wrap gap-2">
			{!! components.Badge(components.BadgeProps{Label: .Rating.Status, Variant: rating.StatusVariant(.Rating.StatusKey)}) !!}
			@if(.Rating.UnderReview)
				{!! components.Badge(components.BadgeProps{Label: .Labels.T("screen.under_review"), Variant: "outline"}) !!}
			@endif
		</div>
	</header>

	@if(.Rating.UnderReview)
		<div class="mt-4">{!! components.Alert(components.AlertProps{Title: .Labels.T("screen.under_review"), Message: .Labels.T("screen.under_review_lead")}) !!}</div>
	@endif
	@if(.Rating.ModerationNote != "")
		<div class="mt-4">{!! components.Alert(components.AlertProps{Variant: "destructive", Title: .Labels.T("screen.moderation_title"), Message: .Rating.ModerationNote}) !!}</div>
	@endif

	<div class="card mt-6 grid gap-3 p-5">
		{!! components.Rating(scoreStars(.)) !!}
		@if(.Rating.Body != "")
			<p class="text-sm break-words whitespace-pre-line">{{ .Rating.Body }}</p>
		@endif
		<dl class="text-muted-foreground flex flex-wrap gap-x-6 gap-y-1 text-sm">
			<div class="flex gap-1.5"><dt>{{ .Labels.T("field.created_at") }}</dt><dd>{{ .Rating.Created }}</dd></div>
			@if(.Rating.Edited != "")
				<div class="flex gap-1.5"><dt>{{ .Labels.T("field.edited_at") }}</dt><dd>{{ .Rating.Edited }}</dd></div>
			@endif
			@if(.Rating.Moderated != "")
				<div class="flex gap-1.5"><dt>{{ .Labels.T("field.moderated_at") }}</dt><dd>{{ .Rating.Moderated }}</dd></div>
			@endif
		</dl>
	</div>

	@if(.CanEdit)
		<form class="card mt-6 grid gap-4 p-5" method="post" action="{{ .UpdateURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.edit_title") }}</h2>
			@if(.HasErrors())
				<ul class="text-destructive grid gap-1 text-sm" role="alert">
					@foreach(.All() as line)
						<li>{{ line }}</li>
					@endforeach
				</ul>
			@endif
			{!! components.Rating(components.RatingProps{Name: "score", Label: .Labels.T("field.score"), Value: .Rating.ScoreText, Required: true, OptionLabels: .Labels.StarNames(), Page: .Form()}) !!}
			{!! components.Textarea(components.TextareaProps{Name: "body", Label: .Labels.T("field.body"), Value: .Rating.Body, Hint: .CharsHint(.MaxChars, .BodyRequire), Rows: 6, Required: .BodyRequire, Page: .Form()}) !!}
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}</div>
		</form>
	@endif

	@if(.CanWithdraw)
		<div class="mt-6">
			{!! components.DialogTrigger(components.DialogTriggerProps{Dialog: "withdraw-rating", Label: .Labels.T("control.withdraw"), Variant: "outline"}) !!}
		</div>
		{!! components.FormDialog(components.FormDialogProps{
			ID: "withdraw-rating", Title: .Labels.T("screen.withdraw_title"), Description: .Labels.T("screen.withdraw_lead"),
			Action: .WithdrawURL, SubmitLabel: .Labels.T("control.withdraw"), SubmitVariant: "destructive",
			CancelLabel: .Labels.T("control.cancel"), CloseLabel: .Labels.T("control.close"), Alert: true, Page: .Page,
		}) !!}
	@endif
</section>
@endsection
