package egret

// Handlers is the default set of global filters.
// It may be set by the application on initialization.
var SharedHandlers = []HandlerFunc{
	PanicHandler,    // Recover from panics and display an error page instead.
	SessionHandler,  // Restore and write the session cookie.
	FlashHandler,    // Restore and write the flash cookie.
	CompressHandler, // Compress the result.
}

// NilHandler and NilChain are helpful in writing filter tests.
var (
	NilHandler = func(_ []HandlerFunc) {}
)
