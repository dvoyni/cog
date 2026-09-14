package jsstorage

// Config configures the browser permanent filesystem. It is supplied under
// Name. A browser has no executable to take a name from, so its zero value, an
// empty AppId, fails Register with ErrInvalidAppId.
type Config struct {
	// AppId is part of the localStorage key, cog.storage.<AppId>. It must be a
	// single name: not empty, not . or .., and without / or \. It is never
	// defaulted.
	AppId string
}
