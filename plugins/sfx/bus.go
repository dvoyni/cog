package sfx

// Bus is a group of voices sharing one volume, declared by the game as its
// own constants. Every bus sits directly under Master, and buses do not nest.
// Config.MaxBuses of them exist, Master among them; a bus outside that range
// is Master everywhere a bus is named, a Play, a SetBus, a StopBus and a
// read, rather than an error, so a mis-declared constant does not silence a
// game: its sounds play on Master and its slider moves Master.
type Bus int

// Master is the only bus sfx declares, the one a voice that names no bus
// plays on, so a game that declares no buses still has a working master
// slider. Stopping it stops every voice.
const Master Bus = 0

// resolve answers the bus b is among count buses: b itself when it is in
// range, Master when it is not.
func (b Bus) resolve(count int) Bus {
	if b < 0 || int(b) >= count {
		return Master
	}
	return b
}
