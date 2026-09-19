package types

// The System's half of the Hooks pace check: hooks.md § Validation mode
// checks the rule. Everything here is reached only from behind `if validate`,
// so a release build compiles none of it into a path that runs. The reader's
// half is Hooks.keepPace, in hooks.go; the List checks, which do need a table,
// are in validate_on.go.

// hookPaceLimit is how many counted runs may pass between two runs of a Hooks
// reader: hooks.md publishes it as 16, so a writer that runs twice in a tick to
// catch up a fixed step does not trip it.
const hookPaceLimit = 16

// pacedLog is one log and where it ended when this run started.
type pacedLog struct {
	log *hookRecords
	end uint64
}

// systemPace is a System's count of its own runs on the Hook logs it can append
// to: each run that grew a log bumps that log's counter once, however many
// records it appended.
//
// A System appends only to the logs of Stores it holds a write lock on, so it
// reads each one only while nothing else appends to it or compacts it.
type systemPace struct {
	// stores are the Stores the System writes, named at registration; every
	// says it holds write{*Entities} and so may append to any log, through a
	// Spawn or a Despawn. Neither is resolved to logs until the first run,
	// because a Store's log exists only once a reader registers, which may be
	// after this System.
	stores   []*storeHeader
	every    bool
	resolved bool
	logs     []pacedLog
}

// enrol names a Store the System writes.
func (p *systemPace) enrol(store *storeHeader) {
	for _, known := range p.stores {
		if known == store {
			return
		}
	}
	p.stores = append(p.stores, store)
}

// begin marks where each log ends as the run starts. A log's end is counted
// from the first record it ever held, so compaction does not move it and only
// an append does.
func (p *systemPace) begin(en *Entities) {
	if !p.resolved {
		p.resolve(en)
	}
	for i := range p.logs {
		paced := &p.logs[i]
		paced.end = paced.log.base + uint64(len(paced.log.records))
	}
}

// end counts the run once on every log it appended to. It runs after the
// System's Changed compares, which append too.
func (p *systemPace) end() {
	for i := range p.logs {
		paced := &p.logs[i]
		if paced.log.base+uint64(len(paced.log.records)) != paced.end {
			paced.log.runs++
		}
	}
}

func (p *systemPace) resolve(en *Entities) {
	p.resolved = true
	if p.every {
		for _, class := range en.classes {
			if class.header.hooks != nil {
				p.logs = append(p.logs, pacedLog{log: class.header.hooks})
			}
		}
		return
	}
	for _, store := range p.stores {
		if store.hooks != nil {
			p.logs = append(p.logs, pacedLog{log: store.hooks})
		}
	}
}
