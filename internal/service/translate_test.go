package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/config"
)

func TestNewTranslatorSelectsProvider(t *testing.T) {
	cases := []struct {
		name      string
		cfg       config.TranslateConfig
		provider  string
		available bool
	}{
		{"default is unavailable", config.TranslateConfig{}, "none", false},
		{"explicit none", config.TranslateConfig{Provider: "none"}, "none", false},
		{"claude", config.TranslateConfig{Provider: "claude", AnthropicKey: "sk-test"}, "claude", true},
		{"libretranslate", config.TranslateConfig{Provider: "libretranslate", LibreURL: "http://lt:5000"}, "libretranslate", true},
		{"libretranslate without a url is unavailable", config.TranslateConfig{Provider: "libretranslate"}, "libretranslate", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTranslator(tc.cfg)
			if tr == nil {
				t.Fatal("NewTranslator returned nil; it must always return a usable value")
			}
			if got := tr.Provider(); got != tc.provider {
				t.Errorf("provider = %q, want %q", got, tc.provider)
			}
			if got := tr.Available(); got != tc.available {
				t.Errorf("available = %v, want %v", got, tc.available)
			}
		})
	}
}

// With nothing configured the app must still run; callers get a typed error
// rather than a nil dereference.
func TestUnavailableTranslatorReportsTypedError(t *testing.T) {
	tr := NewTranslator(config.TranslateConfig{})
	_, err := tr.Translate(context.Background(), []string{"hello"}, "en", "ar", "")
	if !errors.Is(err, ErrTranslatorUnavailable) {
		t.Errorf("got %v, want ErrTranslatorUnavailable", err)
	}
}

func TestParseTranslationItems(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			"plain object",
			`{"items": ["واحد", "اثنان"]}`,
			[]string{"واحد", "اثنان"},
		},
		{
			"fenced json",
			"```json\n{\"items\": [\"un\", \"deux\"]}\n```",
			[]string{"un", "deux"},
		},
		{
			"bare fence",
			"```\n{\"items\": [\"a\"]}\n```",
			[]string{"a"},
		},
		{
			"prose either side",
			"Here are the translations:\n{\"items\": [\"x\", \"y\"]}\nLet me know if you need more.",
			[]string{"x", "y"},
		},
		{
			"empty list is valid",
			`{"items": []}`,
			[]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTranslationItems(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d items %q, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("item %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseTranslationItemsRejectsGarbage(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"I cannot translate that.",
		"{not json at all",
	} {
		if _, err := parseTranslationItems(in); err == nil {
			t.Errorf("input %q should have produced an error", in)
		}
	}
}

func TestLanguageName(t *testing.T) {
	want := map[string]string{"ar": "Arabic", "en": "English", "fr": "French", "de": "de"}
	for code, name := range want {
		if got := LanguageName(code); got != name {
			t.Errorf("LanguageName(%q) = %q, want %q", code, got, name)
		}
	}
}

// stubTranslator lets the content-level helpers be tested without a provider.
type stubTranslator struct {
	lastTexts []string
	lastHint  string
	reply     []string
	err       error
}

func (s *stubTranslator) Provider() string { return "stub" }
func (s *stubTranslator) Available() bool  { return true }
func (s *stubTranslator) Translate(_ context.Context, texts []string, _, _, hint string) ([]string, error) {
	s.lastTexts = texts
	s.lastHint = hint
	if s.err != nil {
		return nil, s.err
	}
	if s.reply != nil {
		return s.reply, nil
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = "»" + t
	}
	return out, nil
}

func TestTranslateQuestionRoundTrip(t *testing.T) {
	stub := &stubTranslator{}
	src := QuestionText{
		Prompt:      "How many surahs are in the Qur'an?",
		Choices:     []string{"110", "112", "114", "116"},
		Explanation: "The Qur'an contains 114 surahs.",
	}

	got, err := TranslateQuestion(context.Background(), stub, src, "en", "ar")
	if err != nil {
		t.Fatal(err)
	}

	// The whole question must travel as one batch, so the provider can keep
	// the choices distinct from each other.
	if len(stub.lastTexts) != 6 {
		t.Fatalf("sent %d strings, want 6 (prompt + 4 choices + explanation)", len(stub.lastTexts))
	}
	if stub.lastTexts[0] != src.Prompt {
		t.Error("the prompt must be the first item")
	}
	if stub.lastTexts[5] != src.Explanation {
		t.Error("the explanation must be the last item")
	}
	if !strings.Contains(stub.lastHint, "answer choices") {
		t.Errorf("hint should describe the batch, got %q", stub.lastHint)
	}

	if got.Prompt != "»"+src.Prompt {
		t.Errorf("prompt = %q", got.Prompt)
	}
	if len(got.Choices) != 4 {
		t.Fatalf("got %d choices, want 4", len(got.Choices))
	}
	for i, c := range got.Choices {
		if c != "»"+src.Choices[i] {
			t.Errorf("choice %d = %q", i, c)
		}
	}
	if got.Explanation != "»"+src.Explanation {
		t.Errorf("explanation = %q", got.Explanation)
	}
}

// A provider that drops or invents an item would silently corrupt a question,
// so a count mismatch has to be an error rather than a partial write.
func TestTranslateQuestionRejectsWrongCount(t *testing.T) {
	stub := &stubTranslator{reply: []string{"only", "three", "items"}}
	_, err := TranslateQuestion(context.Background(), stub, QuestionText{
		Prompt:  "q",
		Choices: []string{"a", "b", "c", "d"},
	}, "en", "fr")

	if !errors.Is(err, ErrTranslationShape) {
		t.Errorf("got %v, want ErrTranslationShape", err)
	}
}

func TestTranslateQuestionPropagatesProviderError(t *testing.T) {
	sentinel := errors.New("provider exploded")
	stub := &stubTranslator{err: sentinel}
	_, err := TranslateQuestion(context.Background(), stub, QuestionText{
		Prompt: "q", Choices: []string{"a", "b", "c", "d"},
	}, "en", "ar")

	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want the provider error", err)
	}
}

func TestMissingLocales(t *testing.T) {
	got := MissingLocales(map[string]bool{"ar": true})
	if len(got) != 2 {
		t.Fatalf("got %v, want the two locales that are absent", got)
	}
	for _, code := range got {
		if code == "ar" {
			t.Error("a present locale must not be reported as missing")
		}
	}

	if all := MissingLocales(nil); len(all) != 3 {
		t.Errorf("with nothing present all three locales are missing, got %v", all)
	}
	if none := MissingLocales(map[string]bool{"ar": true, "en": true, "fr": true}); len(none) != 0 {
		t.Errorf("with everything present nothing is missing, got %v", none)
	}
}
