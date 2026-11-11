package support

import "testing"

// The six topics of the picker are the six values of the ticket_topic enum.
func TestTopics(t *testing.T) {
	if len(Topics) != 6 {
		t.Fatalf("got %d topics, want 6", len(Topics))
	}
	for _, topic := range Topics {
		if TopicIcon[topic] == "" {
			t.Errorf("topic %q has no icon", topic)
		}
		if !ValidTopic(string(topic)) {
			t.Errorf("topic %q is not accepted by ValidTopic", topic)
		}
	}
	if ValidTopic("nonsense") {
		t.Error("ValidTopic accepted an unknown topic")
	}
}
