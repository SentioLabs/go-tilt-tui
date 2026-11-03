package theme

// --- Contract assertions ---

var (
	_ func(string) (Theme, bool)   = ByName
	_ func(string) (Theme, string) = Select
)
