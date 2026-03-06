package openclaw

import (
	"fmt"
	"time"
)

func NewScheduler(engine *Engine) *Scheduler {
	return &Scheduler{
		engine: engine,
		ticker: time.NewTicker(time.Duration(engine.config.Interval) * time.Hour),
		stopCh: make(chan struct{}),
	}
}

func (s *Scheduler) Start() {
	for {
		select {
		case <-s.ticker.C:
			if err := s.engine.RunAnalysis(); err != nil {
				fmt.Printf("OpenClaw analysis error: %v\n", err)
			}
		case <-s.stopCh:
			return
		}
	}
}

func (s *Scheduler) Stop() {
	s.ticker.Stop()
	close(s.stopCh)
}
