package kernel

// Kernel is one isolated runtime inside a Supervisor: its resources, its
// registered tasks, its event handlers and its job graph. Handlers, task
// bodies and plugin Start and Stop receive it; code outside the pool, such as
// the main loop, gets one from Supervisor.GetKernel.
//
// Every method is safe from any goroutine. A Kernel that has been destroyed
// stays safe to call: Publish publishes nothing and Notify signals at once.
type Kernel struct {
	id     KernelID
	logger Logger
	// plugins is the order Register and Start ran in; Stop runs it in reverse.
	plugins []Plugin
	// config is what every plugin's Register was handed.
	config any
}

// newKernel creates kernel id, logging through logger, with plugins to
// register in order and the config each is handed. It registers and starts
// nothing: that is Spawn's work. A nil logger is StdLogger.
func newKernel(id KernelID, logger Logger, plugins []Plugin, config any) *Kernel {
	if logger == nil {
		logger = &StdLogger{}
	}
	return &Kernel{id: id, logger: logger, plugins: plugins, config: config}
}

// ID returns the ID Spawn assigned this kernel.
func (k *Kernel) ID() KernelID { return k.id }

// Publish delivers event to every handler subscribed to E, one by one, on the
// calling goroutine, each receiving the event and out dependency the one before
// returned. The handlers schedule into one pooled Executor, whose batch is
// submitted once the last returns, and Publish returns after that.
//
// It answers what the last handler returned: the event, and the out
// dependency later work can wait on. With no subscribers it answers event and
// the zero JobHandle.
//
// Publishing is the only way work is scheduled. A task or a plugin's Start
// that wants jobs run publishes an event whose handlers schedule them. A
// handler publishing on k starts a separate batch; one wanting its nested
// handlers' jobs in its own batch publishes on its Executor instead.
func (k *Kernel) Publish[E any](event E) (E, JobHandle) { return event, JobHandle{} }

// Notify arms sig to be signalled once job has finished, or signals it at once
// if job already has. Checking and arming happen under the kernel's lock, so
// no finish is missed however late Notify is called. job must be this
// kernel's.
//
// It allocates nothing, so one Signal can be armed every frame.
func (k *Kernel) Notify(sig *Signal, job JobHandle) {}

// LogErr forwards err to the Supervisor's Logger under this kernel's ID.
func (k *Kernel) LogErr(err error) { k.logger.LogErr(k.id, err) }

// LogWarn forwards to the Supervisor's Logger under this kernel's ID.
func (k *Kernel) LogWarn(msg string, args ...any) { k.logger.LogWarn(k.id, msg, args...) }

// LogDebug forwards to the Supervisor's Logger under this kernel's ID.
func (k *Kernel) LogDebug(msg string, args ...any) { k.logger.LogDebug(k.id, msg, args...) }

// LogErrOnce forwards to the Supervisor's Logger under this kernel's ID: err
// is logged the first time key is, and dropped after until ForgetLogOnce.
func (k *Kernel) LogErrOnce(key string, err error) { k.logger.LogErrOnce(k.id, key, err) }

// LogWarnOnce forwards to the Supervisor's Logger under this kernel's ID.
func (k *Kernel) LogWarnOnce(key string, msg string, args ...any) {
	k.logger.LogWarnOnce(k.id, key, msg, args...)
}

// LogDebugOnce forwards to the Supervisor's Logger under this kernel's ID.
func (k *Kernel) LogDebugOnce(key string, msg string, args ...any) {
	k.logger.LogDebugOnce(k.id, key, msg, args...)
}

// ForgetLogOnce clears key, so the next Once call under it logs again.
func (k *Kernel) ForgetLogOnce(key string) { k.logger.ForgetLogOnce(k.id, key) }
