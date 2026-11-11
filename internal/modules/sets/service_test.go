package sets

import "testing"

// MQ-02: a set is playable at five questions, and the editor refuses a question
// without its main language or without four answers.
func TestQuestionInputValidate(t *testing.T) {
	full := map[string]Text{"en": {Body: "Q", Answers: []string{"a", "b", "c", "d"}}}
	ok := QuestionInput{Difficulty: 1, CorrectIndex: 0, MainLang: "en", Text: full}
	if err := ok.validate(); err != nil {
		t.Fatalf("a complete question was refused: %v", err)
	}
	noMain := ok
	noMain.Text = map[string]Text{"fr": {Body: "Q", Answers: []string{"a", "b", "c", "d"}}}
	if err := noMain.validate(); err == nil {
		t.Error("a question without its main language was accepted")
	}
	short := ok
	short.Text = map[string]Text{"en": {Body: "Q", Answers: []string{"a", "b"}}}
	if err := short.validate(); err == nil {
		t.Error("a question with two answers was accepted")
	}
	badIndex := ok
	badIndex.CorrectIndex = 9
	if err := badIndex.validate(); err == nil {
		t.Error("correct_index 9 was accepted")
	}
}
