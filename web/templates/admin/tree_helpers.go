package admin

// first falls back when a node carries no icon or colour of its own: the tree must
// still draw a row rather than an empty square.
func first(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// statusBadge is the colour each node status wears. It mirrors the words the
// question table uses, so one status reads the same wherever it appears.
func statusBadge(status string) string {
	switch status {
	case "published":
		return "badge-live"
	case "review":
		return "badge-pending"
	case "archived":
		return "badge-retired"
	default:
		return "badge-draft"
	}
}
