package openclaw

import (
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
