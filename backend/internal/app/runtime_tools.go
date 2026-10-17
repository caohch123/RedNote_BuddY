package app

import (
	"time"

	"xhs-workbench/internal/providers"
)

func (a *App) nodeToolPath() (string, error) {
	s, err := a.settings()
	return s.NodePath, err
}

// Call only while the account operation lock is held. Saved credentials are
// preserved and lazily restored; an active publish/login cannot be interrupted.
func (a *App) refreshNodeRuntimes() {
	if x := a.xhs; x != nil {
		_ = x.rpc.Close()
		x.mu.Lock()
		x.rpc = providers.NewXHSPublisher(a.Root, a.nodeToolPath)
		x.loaded = false
		x.generation = ID()
		x.logins = map[string]*xhsLogin{}
		x.tokens = map[string]xhsPublishToken{}
		x.lastCheck = time.Time{}
		x.mu.Unlock()
	}
}
