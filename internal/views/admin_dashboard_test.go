package views

import (
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// A browser string reduced to the one line staff read. The order of the probes
// is the whole substance of it: Edge's string contains "Chrome", Chrome's
// contains "Safari", and an iPad in desktop mode contains "Macintosh" — so a
// checker that tests the general name first reports every browser as Safari
// and every tablet as a Mac.
func TestDescribeDevicePicksTheSpecificNameFirst(t *testing.T) {
	cases := []struct {
		name  string
		agent string
		want  string
	}{
		{
			"an iPhone, the answer to 'it does not work on my phone'",
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1",
			"Safari · iPhone",
		},
		{
			"Chrome on Android, not Safari on Android",
			"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36",
			"Chrome · Android",
		},
		{
			"Edge, whose string also says Chrome and Safari",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0",
			"Edge · Windows",
		},
		{
			"an iPad, whose string also says Macintosh",
			"Mozilla/5.0 (iPad; CPU OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/604.1",
			"Safari · iPad",
		},
		{
			"Firefox on a Mac",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:125.0) Gecko/20100101 Firefox/125.0",
			"Firefox · Mac",
		},
		{
			// Nothing recognised says nothing, rather than guessing. The raw
			// string sits beside this in the markup for whoever needs it.
			"something unrecognised",
			"curl/8.4.0",
			"",
		},
		{"nothing at all", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeDevice(tc.agent); got != tc.want {
				t.Errorf("describeDevice(%q) = %q, want %q", tc.agent, got, tc.want)
			}
		})
	}
}

// A figure with nothing to compare against has no percentage — everything is
// an infinite rise from nothing — so it must report unknown rather than a
// number the reader cannot interpret.
func TestDeltaRefusesToDivideByAnEmptyPeriod(t *testing.T) {
	if d := delta(120, 0); d.Known {
		t.Errorf("a prior of zero produced a comparison: %+v", d)
	}
	if d := delta(0, 0); d.Known {
		t.Errorf("nothing against nothing produced a comparison: %+v", d)
	}

	// No change is a known result and not a rise: an up arrow on an unchanged
	// figure is the one reading that is actively wrong.
	flat := delta(50, 50)
	if !flat.Known || !flat.Flat || flat.Up {
		t.Errorf("an unchanged figure came back as %+v", flat)
	}
	if flat.Class() != "" {
		t.Errorf("an unchanged figure is coloured %q", flat.Class())
	}

	up, down := delta(120, 100), delta(80, 100)
	if !up.Up || up.Label != "20.0%" {
		t.Errorf("a rise came back as %+v", up)
	}
	if down.Up || down.Label != "20.0%" {
		t.Errorf("a fall came back as %+v", down)
	}
}

// A colour written into a style attribute comes from a category's own column,
// which is the one place markup here is built from stored data — so anything
// that is not a plain hex colour has to be dropped rather than passed through.
func TestCatColourOnlyAcceptsAHexColour(t *testing.T) {
	for _, ok := range []string{"#fff", "#0ea5a4", "#ABCDEF"} {
		if got := catColour(ok); got != "--c:"+ok {
			t.Errorf("catColour(%q) = %q, want it kept", ok, got)
		}
	}
	for _, bad := range []string{
		"", "red", "#12", "#1234567",
		"#fff;background:url(x)", "url(javascript:alert(1))", "var(--brand)",
	} {
		if got := catColour(bad); got != "" {
			t.Errorf("catColour(%q) = %q, want it dropped", bad, got)
		}
	}
}

// The bar on the rated-poorly screen is the design's thumbs split filled from
// a one-to-five average, so the two ends of the scale have to map to the two
// ends of the bar.
func TestRatingShareSpansTheScale(t *testing.T) {
	if got := ratingShare(float64(models.MaxStars)); got != 100 {
		t.Errorf("a perfect average fills %d%% of the bar", got)
	}
	if got := ratingShare(0); got != 0 {
		t.Errorf("an average of zero fills %d%% of the bar", got)
	}
	// Out of range cannot overflow the bar in either direction.
	if got := ratingShare(99); got != 100 {
		t.Errorf("an impossible average fills %d%%", got)
	}
	if got := ratingShare(-3); got != 0 {
		t.Errorf("a negative average fills %d%%", got)
	}
}
