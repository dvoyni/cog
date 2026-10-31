package internal

import "github.com/dvoyni/cog/kernel"

// closeCmd uploads what is pending and closes the Backend. It is a command
// rather than a call from Stop because the write lock it takes is what keeps
// it from racing an update still in flight.
type closeCmd kernel.Command[closeRequest, closeResponse]

type closeRequest struct{}

type closeResponse struct{}

func (p *plugin) registerCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[UnlockAchievementsCmd](p.unlockAchievementsCmdImpl)
	registrar.HandleCommand[SetStatsCmd](p.setStatsCmdImpl)
	registrar.HandleCommand[LanguageCmd](p.languageCmdImpl)
	registrar.HandleCommand[PlayerNameCmd](p.playerNameCmdImpl)
	registrar.HandleCommand[ShowTextInputCmd](p.showTextInputCmdImpl)
	registrar.HandleCommand[HideTextInputCmd](p.hideTextInputCmdImpl)
	registrar.HandleCommand[closeCmd](p.closeCmdImpl)
}

func (p *plugin) unlockAchievementsCmdImpl() (kernel.Lock, kernel.Execute[UnlockAchievementsRequest, UnlockAchievementsResponse]) {
	var current kernel.Write[*state]
	return func(access kernel.ResourceAccess) {
			current = access.GetWrite[*state]()
		}, func(k kernel.Kernel, request UnlockAchievementsRequest) UnlockAchievementsResponse {
			s := current.Get()
			if s.closed {
				return UnlockAchievementsResponse{}
			}
			for _, name := range request.Names {
				if err := p.backend.Get().UnlockAchievement(name); err != nil {
					k.ReportErrorOnce(refusedAchievement(name), err)
					continue
				}
				s.dirty = true
			}
			return UnlockAchievementsResponse{}
		}
}

func (p *plugin) setStatsCmdImpl() (kernel.Lock, kernel.Execute[SetStatsRequest, SetStatsResponse]) {
	var current kernel.Write[*state]
	return func(access kernel.ResourceAccess) {
			current = access.GetWrite[*state]()
		}, func(k kernel.Kernel, request SetStatsRequest) SetStatsResponse {
			s := current.Get()
			if s.closed {
				return SetStatsResponse{}
			}
			for name, value := range request.Stats {
				if err := p.backend.Get().SetStat(name, value); err != nil {
					k.ReportErrorOnce(refusedStat(name), err)
					continue
				}
				s.dirty = true
			}
			return SetStatsResponse{}
		}
}

func (p *plugin) languageCmdImpl() (kernel.Lock, kernel.Execute[LanguageRequest, LanguageResponse]) {
	var current kernel.Write[*state]
	return func(access kernel.ResourceAccess) {
			current = access.GetWrite[*state]()
		}, func(_ kernel.Kernel, _ LanguageRequest) LanguageResponse {
			if current.Get().closed {
				return LanguageResponse{}
			}
			return LanguageResponse{Language: p.backend.Get().Language()}
		}
}

func (p *plugin) playerNameCmdImpl() (kernel.Lock, kernel.Execute[PlayerNameRequest, PlayerNameResponse]) {
	var current kernel.Write[*state]
	return func(access kernel.ResourceAccess) {
			current = access.GetWrite[*state]()
		}, func(_ kernel.Kernel, _ PlayerNameRequest) PlayerNameResponse {
			if current.Get().closed {
				return PlayerNameResponse{}
			}
			return PlayerNameResponse{Name: p.backend.Get().PlayerName()}
		}
}

func (p *plugin) showTextInputCmdImpl() (kernel.Lock, kernel.Execute[ShowTextInputRequest, ShowTextInputResponse]) {
	var current kernel.Write[*state]
	return func(access kernel.ResourceAccess) {
			current = access.GetWrite[*state]()
		}, func(_ kernel.Kernel, request ShowTextInputRequest) ShowTextInputResponse {
			if current.Get().closed {
				return ShowTextInputResponse{}
			}
			return ShowTextInputResponse{Shown: p.backend.Get().ShowTextInput(request.Field)}
		}
}

func (p *plugin) hideTextInputCmdImpl() (kernel.Lock, kernel.Execute[HideTextInputRequest, HideTextInputResponse]) {
	var current kernel.Write[*state]
	return func(access kernel.ResourceAccess) {
			current = access.GetWrite[*state]()
		}, func(_ kernel.Kernel, _ HideTextInputRequest) HideTextInputResponse {
			if !current.Get().closed {
				p.backend.Get().HideTextInput()
			}
			return HideTextInputResponse{}
		}
}

func (p *plugin) closeCmdImpl() (kernel.Lock, kernel.Execute[closeRequest, closeResponse]) {
	var current kernel.Write[*state]
	return func(access kernel.ResourceAccess) {
			current = access.GetWrite[*state]()
		}, func(k kernel.Kernel, _ closeRequest) closeResponse {
			s := current.Get()
			if s.closed {
				return closeResponse{}
			}
			if s.dirty {
				s.store(k, p.backend.Get())
			}
			p.backend.Get().Close()
			s.closed = true
			return closeResponse{}
		}
}
