package gogpu

// Config configures the gogpu driver's window. The fixed step it drives is
// app's Config, not this one. It is supplied under Name, and its zero value is
// the default: a zero field takes the default its comment names, so a caller
// sets only what it changes, directly or with the With* builders (each returns
// a modified copy):
//
//	cfg := gogpu.Config{}.WithTitle("Feuds").WithSize(1600, 900)
//
// Fields are exported so a Config can be serialized. The switches that default
// to on are spelled as their negation (NoResize, NoVSync), so that off is the
// zero value too.
type Config struct {
	// Title is the window title. Empty means "cog".
	Title string
	// Width and Height are the initial logical window size (DIP). Zero means
	// 1280x720; each is defaulted on its own.
	Width, Height int
	// NoResize fixes the window's size. Default: false, a resizable window.
	NoResize bool
	// NoVSync disables vertical sync. Default: false, vsync on.
	NoVSync bool
	// Fullscreen starts the window fullscreen. Default: false.
	Fullscreen bool
	// AppName is the application/menu name (macOS). Default: empty (the gogpu
	// library's default).
	AppName string
}

// WithTitle sets the window title.
func (c Config) WithTitle(title string) Config {
	c.Title = title
	return c
}

// WithSize sets the initial logical window size (DIP).
func (c Config) WithSize(width, height int) Config {
	c.Width, c.Height = width, height
	return c
}

// WithResizable sets whether the window can be resized.
func (c Config) WithResizable(resizable bool) Config {
	c.NoResize = !resizable
	return c
}

// WithVSync sets whether vertical sync is enabled.
func (c Config) WithVSync(vsync bool) Config {
	c.NoVSync = !vsync
	return c
}

// WithFullscreen sets whether the window starts fullscreen.
func (c Config) WithFullscreen(fullscreen bool) Config {
	c.Fullscreen = fullscreen
	return c
}

// WithAppName sets the application/menu name (macOS).
func (c Config) WithAppName(name string) Config {
	c.AppName = name
	return c
}
