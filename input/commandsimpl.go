package input

import "github.com/dvoyni/cog/kernel"

// applyCmdImpl folds a batch of input changes into the State (under its write
// lock) and publishes the discrete event for each change.
func applyCmdImpl() (kernel.Lock, kernel.Execute[ApplyRequest, ApplyResponse]) {
	var state kernel.Write[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*State]()
		}, func(k kernel.Kernel, request ApplyRequest) (ApplyResponse, error) {
			s := state.Get()
			for _, c := range request.Changes {
				s.apply(c)
				publish(k, c)
			}
			return ApplyResponse{}, nil
		}
}
