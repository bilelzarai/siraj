package views

import "github.com/bilelzarai/siraj/internal/models"

// The review pane's small readings of the question it is showing.

// OpenIcon and OpenCategory are the category of the question under review,
// taken from its row in the queue — the draft carries a category id and the
// row carries the name, and the pane shows the name.
func (d AdminReviewData) OpenIcon() string {
	for _, q := range d.Questions {
		if q.ID == d.OpenID {
			return q.CategoryIcon
		}
	}
	return ""
}

func (d AdminReviewData) OpenCategory() string {
	for _, q := range d.Questions {
		if q.ID == d.OpenID {
			return q.CategoryName
		}
	}
	return ""
}

// OpenPending is the languages of the open question that are actually waiting
// on a verdict. Offering all three invited approving a translation nobody had
// written.
func (d AdminReviewData) OpenPending() []string {
	for _, q := range d.Questions {
		if q.ID == d.OpenID {
			return q.PendingLocales
		}
	}
	return nil
}

// NextID is the question after the open one in the queue, which is what "skip"
// moves to. Zero at the end of the list, where there is nothing to skip to.
func (d AdminReviewData) NextID() int {
	for i, q := range d.Questions {
		if q.ID == d.OpenID && i+1 < len(d.Questions) {
			return d.Questions[i+1].ID
		}
	}
	return 0
}

// reviewCheckIcon and reviewCheckClass draw one automatic check. A failure is
// something not to approve past; a warning is something to look at; a pass is
// said quietly, because a list where everything shouts is a list nobody reads.
func reviewCheckIcon(level string) string {
	switch level {
	case models.CheckFail:
		return "i-x-circle"
	case models.CheckWarn:
		return "i-alert"
	default:
		return "i-check-circle"
	}
}

func reviewCheckClass(level string) string {
	switch level {
	case models.CheckFail:
		return "icon-sm check-fail"
	case models.CheckWarn:
		return "icon-sm check-warn"
	default:
		return "icon-sm check-pass"
	}
}
