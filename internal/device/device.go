package device

// Device is the interface every GPIO-controlled device must implement.
type Device interface {
	On(pin int) error
	Off(pin int) error
	ID() string
	Close() error
}
