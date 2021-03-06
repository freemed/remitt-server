package transport

// Transporter is the interface for transport plugins
type Transporter interface {
	// Transport performs the actual work of transport, given the input.
	Transport(string, interface{}) error

	// InputFormat returns the input format required by this plugin.
	InputFormat() string

	// Options returns a list of valid options for this transporter type
	Options() []string

	// SetOptions sets the current options for this plugin
	SetOptions(map[string]interface{}) error
}
