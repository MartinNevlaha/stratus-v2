package openclaw

import (
	"fmt"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

type Engine struct {
	database  *db.DB
	config    config.OpenClawConfig
	scheduler *Scheduler
	analyzer  *Analyzer
	monitor   *Monitor
	stopCh    chan struct{}
}

type Scheduler struct {
	engine *Engine
	ticker *time.Ticker
	stopCh chan struct{}
}

type Analyzer struct {
	engine *Engine
}

type Monitor struct {
	engine *Engine
}

func NewEngine(database *db.DB, cfg config.OpenClawConfig) *Engine {
	return &Engine{
		database:  database,
		config:    cfg,
		scheduler: &Scheduler{},
		analyzer:  &Analyzer{},
		monitor:   &Monitor{},
		stopCh:    make(chan struct{}),
	}
}

func (e *Engine) Start() error {
	state, err := e.database.GetOpenClawState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}

	if state == nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		state = &db.OpenClawState{
			LastAnalysis:       now,
			NextAnalysis:       now,
			PatternsDetected:   0,
			ProposalsGenerated: 0,
			ProposalsAccepted:  0,
			AcceptanceRate:     0,
			ModelVersion:       "v1",
			ConfigJSON:         "{}",
		}

		if err := e.database.SaveOpenClawState(state); err != nil {
			return fmt.Errorf("init state: %w", err)
		}
	}

	go e.scheduler.Start()

	return nil
}

func (e *Engine) Stop() {
	close(e.stopCh)
	e.scheduler.Stop()
}
