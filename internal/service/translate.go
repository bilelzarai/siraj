package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/bilelzarai/siraj/internal/config"
	"github.com/bilelzarai/siraj/internal/i18n"
)

var (
	ErrTranslatorUnavailable = errors.New("no translation provider configured")
	ErrTranslationShape      = errors.New("translator returned the wrong number of strings")
)

// Translator turns a batch of related strings into another language. The batch
// is translated as one unit so a provider sees a question together with its
// answer choices rather than as unrelated fragments.
type Translator interface {
	Provider() string
	Available() bool
	// Translate returns exactly one output per input, in the same order.
	// hint describes what the batch is, which materially improves quality.
	Translate(ctx context.Context, texts []string, from, to, hint string) ([]string, error)
}

// NewTranslator builds the configured provider. It never returns nil: with no
// provider configured the caller gets one that reports Available() == false.
func NewTranslator(cfg config.TranslateConfig) Translator {
	switch cfg.Provider {
	case "claude":
		return newClaudeTranslator(cfg)
	case "libretranslate":
		return &libreTranslator{url: cfg.LibreURL, key: cfg.LibreKey}
	default:
		return unavailableTranslator{}
	}
}

// LanguageName spells a locale out for a translation prompt; a bare code is
// ambiguous to some providers.
func LanguageName(code string) string {
	switch code {
	case "ar":
		return "Arabic"
	case "en":
		return "English"
	case "fr":
		return "French"
	default:
		return code
	}
}

// ---------------------------------------------------------- no provider --

type unavailableTranslator struct{}

func (unavailableTranslator) Provider() string { return "none" }
func (unavailableTranslator) Available() bool  { return false }
func (unavailableTranslator) Translate(context.Context, []string, string, string, string) ([]string, error) {
	return nil, ErrTranslatorUnavailable
}

// -------------------------------------------------------------- Claude --

// claudeTranslator uses the Anthropic Messages API. Religious terminology is
// where generic translation engines do worst, so the system prompt is specific
// about transliteration and about not paraphrasing scripture.
type claudeTranslator struct {
	client anthropic.Client
	model  string
}

func newClaudeTranslator(cfg config.TranslateConfig) *claudeTranslator {
	model := cfg.AnthropicModel
	if model == "" {
		model = "claude-opus-5"
	}
	return &claudeTranslator{
		client: anthropic.NewClient(option.WithAPIKey(cfg.AnthropicKey)),
		model:  model,
	}
}

func (c *claudeTranslator) Provider() string { return "claude" }
func (c *claudeTranslator) Available() bool  { return true }

const claudeTranslateSystem = `You translate content for an Islamic educational quiz.

Rules:
- Translate meaning, not words. The result must read naturally to a native speaker.
- Preserve Islamic terminology precisely. Use the conventional rendering in the
  target language (e.g. salah/prière, zakat, hajj, surah/sourate, hadith, Sīrah).
- Never paraphrase or re-translate a Qur'anic verse or a hadith. Reproduce the
  wording that is conventional in the target language, and keep Arabic script
  quotations in Arabic script.
- Keep honorifics such as ﷺ exactly where they appear.
- Keep numbers, dates and proper nouns unchanged apart from normal
  transliteration into the target script.
- Keep each item's length close to the original. Answer choices must stay short
  and must remain clearly distinct from one another.
- Translate every item independently of the others; do not merge, reorder,
  renumber or drop any item.

Return only the translations, one per input item, in the original order.`

func (c *claudeTranslator) Translate(ctx context.Context, texts []string, from, to, hint string) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	payload, err := json.MarshalIndent(map[string]any{
		"context": hint,
		"from":    LanguageName(from),
		"to":      LanguageName(to),
		"items":   texts,
	}, "", "  ")
	if err != nil {
		return nil, err
	}

	prompt := fmt.Sprintf(
		"Translate each item from %s to %s.\n\n%s\n\n"+
			"Reply with a JSON object of the form {\"items\": [...]} containing exactly %d "+
			"translated strings, in the same order. Output nothing else.",
		LanguageName(from), LanguageName(to), payload, len(texts))

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	message, err := c.client.Messages.New(callCtx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{{
			Text: claudeTranslateSystem,
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}

	// Safety classifiers can decline a request; the call still succeeds, so
	// the stop reason has to be checked before reading any content.
	if message.StopReason == anthropic.StopReasonRefusal {
		return nil, fmt.Errorf("translation declined by the model (%s)", message.StopDetails.Category)
	}

	var out strings.Builder
	for _, block := range message.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			out.WriteString(text.Text)
		}
	}

	items, err := parseTranslationItems(out.String())
	if err != nil {
		return nil, err
	}
	if len(items) != len(texts) {
		return nil, fmt.Errorf("%w: wanted %d, got %d", ErrTranslationShape, len(texts), len(items))
	}
	return items, nil
}

// parseTranslationItems reads {"items": [...]} out of a reply, tolerating the
// markdown fence a model sometimes wraps JSON in.
func parseTranslationItems(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty translation response")
	}

	if i := strings.Index(raw, "```"); i >= 0 {
		rest := raw[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		if j := strings.Index(rest, "```"); j >= 0 {
			raw = strings.TrimSpace(rest[:j])
		}
	}

	// Trim any prose that survived either side of the object.
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in translation response: %.120q", raw)
	}

	var payload struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &payload); err != nil {
		return nil, fmt.Errorf("parse translation response: %w", err)
	}
	return payload.Items, nil
}

// ------------------------------------------------------ LibreTranslate --

// libreTranslator talks to a LibreTranslate instance. It translates one string
// per request because the batch endpoint is not available on every deployment.
type libreTranslator struct {
	url string
	key string
}

func (l *libreTranslator) Provider() string { return "libretranslate" }
func (l *libreTranslator) Available() bool  { return l.url != "" }

func (l *libreTranslator) Translate(ctx context.Context, texts []string, from, to, _ string) ([]string, error) {
	if !l.Available() {
		return nil, ErrTranslatorUnavailable
	}

	out := make([]string, 0, len(texts))
	for _, text := range texts {
		translated, err := l.one(ctx, text, from, to)
		if err != nil {
			return nil, err
		}
		out = append(out, translated)
	}
	return out, nil
}

func (l *libreTranslator) one(ctx context.Context, text, from, to string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"q": text, "source": from, "target": to, "format": "text", "api_key": l.key,
	})
	if err != nil {
		return "", err
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost,
		l.url+"/translate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("libretranslate request: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Translated string `json:"translatedText"`
		Error      string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("libretranslate response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || payload.Error != "" {
		return "", fmt.Errorf("libretranslate error (%d): %s", resp.StatusCode, payload.Error)
	}
	return payload.Translated, nil
}

// --------------------------------------------------- content-level API --

// QuestionText is one locale's worth of a question.
type QuestionText struct {
	Prompt      string
	Choices     []string
	Explanation string
}

// TranslateQuestion renders a whole question into another locale in a single
// call, so the provider can keep the answer choices mutually distinct.
func TranslateQuestion(ctx context.Context, t Translator, src QuestionText, from, to string) (QuestionText, error) {
	texts := make([]string, 0, len(src.Choices)+2)
	texts = append(texts, src.Prompt)
	texts = append(texts, src.Choices...)
	texts = append(texts, src.Explanation)

	hint := "A multiple-choice question for an Islamic knowledge quiz: " +
		"the first item is the question, the next four are its answer choices, " +
		"and the last is the explanation shown after answering."

	out, err := t.Translate(ctx, texts, from, to, hint)
	if err != nil {
		return QuestionText{}, err
	}
	if len(out) != len(texts) {
		return QuestionText{}, ErrTranslationShape
	}

	return QuestionText{
		Prompt:      out[0],
		Choices:     out[1 : 1+len(src.Choices)],
		Explanation: out[len(out)-1],
	}, nil
}

// MissingLocales lists the shipped locales a content item does not yet have.
func MissingLocales(present map[string]bool) []string {
	var missing []string
	for _, loc := range i18n.Supported {
		if !present[loc.Code] {
			missing = append(missing, loc.Code)
		}
	}
	return missing
}

// LogTranslatorStatus records which provider is active at boot so an operator
// can see immediately whether automatic translation is actually available.
func LogTranslatorStatus(t Translator) {
	if t.Available() {
		slog.Info("machine translation enabled", "provider", t.Provider())
		return
	}
	slog.Info("machine translation disabled",
		"reason", "no provider configured",
		"hint", "set TRANSLATE_PROVIDER=claude with ANTHROPIC_API_KEY, or =libretranslate with LIBRETRANSLATE_URL")
}
