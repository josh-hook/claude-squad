package extensions

import (
	"claude-squad/log"
	"claude-squad/session"
	"time"
)

// Extension is the interface that all extensions must implement.
type Extension interface {
	// Name returns a human-readable name for logging.
	Name() string

	// Check inspects an idle instance and optionally prompts it.
	// Returns true if it sent a prompt (so remaining extensions are skipped for this instance).
	Check(inst *session.Instance) bool
}

// ExtensionManager runs registered extensions against idle instances on a timer.
type ExtensionManager struct {
	extensions []Extension
	interval   time.Duration
	stopCh     chan struct{}
	instances  func() []*session.Instance
}

// NewExtensionManager creates a manager with all registered extensions.
// snapshotFn is called each tick to get the current instance list.
func NewExtensionManager(snapshotFn func() []*session.Instance) *ExtensionManager {
	return &ExtensionManager{
		extensions: []Extension{
			NewGHActionsExtension(),
		},
		interval:  30 * time.Second,
		stopCh:    make(chan struct{}),
		instances: snapshotFn,
	}
}

// Start begins the background extension loop.
func (m *ExtensionManager) Start() {
	go m.run()
}

// Stop terminates the background loop.
func (m *ExtensionManager) Stop() {
	close(m.stopCh)
}

func (m *ExtensionManager) run() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

func (m *ExtensionManager) tick() {
	instances := m.instances()

	var idle []string
	for _, inst := range instances {
		if inst.Status == session.Ready && inst.Started() && !inst.Paused() {
			idle = append(idle, inst.Title)
		}
	}
	log.InfoLog.Printf("[extensions] tick: %d instances, %d idle %v", len(instances), len(idle), idle)

	for _, inst := range instances {
		if inst.Status != session.Ready {
			continue
		}
		if !inst.Started() || inst.Paused() {
			continue
		}

		for _, ext := range m.extensions {
			log.InfoLog.Printf("[extensions] running %s on %q", ext.Name(), inst.Title)
			if ext.Check(inst) {
				log.InfoLog.Printf("[extensions] %s prompted instance %q", ext.Name(), inst.Title)
				break
			}
		}
	}
}
