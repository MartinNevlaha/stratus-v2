package openclaw

import (
	"time"
)

type Scheduler struct {
	engine *Engine
	ticker *time.Ticker
	stopCh chan struct{}
}

func (s *Scheduler) Start() {
	if s.engine.config.Interval <= 0 {
		s.engine.config.Interval = 1
	}

	s.ticker = time.NewTicker(time.Duration(s.engine.config.Interval) * time.Hour)
	defer s.ticker.Stop()

	for {
		select {
		case <-s.ticker.C:
			s.engine.RunAnalysis()
		case <-s.stopCh:
			return
		}
	}
}

func (s *Scheduler) Stop() {
	s.ticker.Stop()
	close(s.stopCh)
}
