package translation

type Translator interface {
	Translate(interface{}) ([]byte, error)
}
