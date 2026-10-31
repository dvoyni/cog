package internal

import "time"

// DefaultStoreInterval is the shortest gap between two uploads when Config
// names none.
const DefaultStoreInterval = 5 * time.Second

// Config configures platform. It is supplied under Name, and its zero value is
// the default.
type Config struct {
	// StoreInterval is the shortest gap between two uploads. A storefront
	// throttles a game that uploads too often, so changes made inside one
	// interval go up together. Zero means DefaultStoreInterval.
	StoreInterval time.Duration
}

// WithStoreInterval sets the shortest gap between two uploads.
func (c Config) WithStoreInterval(interval time.Duration) Config {
	c.StoreInterval = interval
	return c
}
