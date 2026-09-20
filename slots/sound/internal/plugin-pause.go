package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/internal/types"
)

// suspendOnPauseChange is what an engine Pause does to sound, and it is the
// reason a game does nothing: a paused game that keeps playing footsteps is a
// bug in every game that hits it, so audio subscribes rather than every game
// remembering to.
//
// It suspends rather than silences. The playhead stops and resumes on the same
// sample, which is why it travels as VoiceParams.Paused and not as a gain of
// zero: a resident buffer started with start() keeps advancing whatever its
// gain is, and a resume would land thirty seconds in.
//
// It exists as its own subscription because pause stops app.UpdateEvent, and
// the flush that would otherwise carry this runs on that event. Under pause the
// Adapter would keep mixing the last batch it was given, forever, so something
// has to reach it while no tick is running - and an engine Pause is exactly one
// batch of updates, at most MaxVoices of them, with no special path at the
// seam.
//
// Its lock set is Voices and the scratch batch, and deliberately not the four
// the flush takes: the Queue is not drained here and no Clip is touched. The
// write lock on Voices is what serializes this Emit against the flush's - both
// take it, so the Adapter is never called from two places at once.
func (p *plugin) suspendOnPauseChange() (kernel.Lock, kernel.Observe[app.PauseChangeEvent]) {
	var voices kernel.Write[*sound.Voices]
	var scratch kernel.Write[*flushScratch]
	return func(access kernel.ResourceAccess) {
			voices = access.GetWrite[*sound.Voices]()
			scratch = access.GetWrite[*flushScratch]()
		}, func(_ kernel.Kernel, event app.PauseChangeEvent) {
			live := voices.Get()
			if !types.VoicesSetEnginePaused(live, event.Paused) {
				return
			}
			work := scratch.Get()
			types.BatchReset(&work.batch)
			// Not a resync: a pause change is not the Device arriving, so
			// this restates only the Voices the pause moved.
			types.VoicesCollect(live, &work.batch, false)
			p.backend.Get().Emit(&work.batch)
			types.VoicesEndTick(live)
		}
}
