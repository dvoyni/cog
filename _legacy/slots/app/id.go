package app

import "github.com/dvoyni/cog/slots/app/internal"

// Name is the app plugin name and its configuration key. A plugin that
// dispatches QuitCmd or TimeCmd declares it as a dependency, which is what
// guarantees the commands a handler.
const Name = internal.Name
