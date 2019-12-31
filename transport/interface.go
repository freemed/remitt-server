package transport

// Transporter is the interface for transport plugins
type Transporter interface {
	// Transport performs the actual work of transport, given the input.
	Transport(interface{}) error

	// InputFormat returns the input format required by this plugin.
	InputFormat() string
}
