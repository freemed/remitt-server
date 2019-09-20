package transmission

// Transmitter is the interface for transmission plugins
type Transmitter interface {
	// Translate performs the actual work of transmission, given the input.
	Transmit(interface{}) error

	// InputFormat returns the input format required by this plugin.
	InputFormat() string
}
