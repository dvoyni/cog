package input

import "github.com/dvoyni/cog/kernel"

// ApplyCmd applies a batch of input Changes to the State (under its write lock)
// and publishes the discrete input events.
type ApplyCmd kernel.Command[ApplyRequest, ApplyResponse]

// ApplyRequest is the request for ApplyCmd: the batch of input changes to fold in.
type ApplyRequest struct{ Changes []Change }

// ApplyResponse is the (empty) response from ApplyCmd.
type ApplyResponse struct{}
