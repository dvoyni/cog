package rendertest

import "github.com/dvoyni/cog/gfx"

// laggingInstance is the harness's recording instance with each adapter it
// answers lagging, so each device they answer finishes its GPU work a beat
// late.
type laggingInstance struct {
	gfx.InstanceSocket
	harness *Harness
}

// RequestAdapter answers the recording instance's adapter, lagging.
func (i laggingInstance) RequestAdapter(options gfx.RequestAdapterOptions) (gfx.BackendAdapter, error) {
	adapter, err := i.InstanceSocket.RequestAdapter(options)
	if err != nil {
		return nil, err
	}
	return laggingAdapter{BackendAdapter: adapter, harness: i.harness}, nil
}
