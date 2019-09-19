package translation

// Translator is the interface for translation plugins
type Translator interface {
	// Translate performs the actual work of translation, given the input.
	Translate(interface{}) ([]byte, error)

	// Resolver takes an input and an output and returns whether or not the
	// current translator matches the criteria.
	Resolver(string, string) bool
}
