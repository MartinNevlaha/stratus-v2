package workflow_synthesis

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

var (
	globalRand   *rand.Rand
	globalRandMu sync.Mutex
)

func init() {
	globalRand = rand.New(rand.NewSource(time.Now().UnixNano()))
}

type ThompsonBandit struct {
	CandidatePulls  int
	BaselinePulls   int
	CandidateReward float64
	BaselineReward  float64
}

func NewThompsonBandit() *ThompsonBandit {
	return &ThompsonBandit{
		CandidatePulls:  1,
		BaselinePulls:   1,
		CandidateReward: 0.5,
		BaselineReward:  0.5,
	}
}

func (b *ThompsonBandit) Select() bool {
	candidateAlpha := b.CandidateReward + 1
	candidateBeta := float64(b.CandidatePulls) - b.CandidateReward + 1
	baselineAlpha := b.BaselineReward + 1
	baselineBeta := float64(b.BaselinePulls) - b.BaselineReward + 1

	candidateSample := betaSample(candidateAlpha, candidateBeta)
	baselineSample := betaSample(baselineAlpha, baselineBeta)

	return candidateSample > baselineSample
}

func (b *ThompsonBandit) Update(wasCandidate bool, success bool) {
	if wasCandidate {
		b.CandidatePulls++
		if success {
			b.CandidateReward++
		}
	} else {
		b.BaselinePulls++
		if success {
			b.BaselineReward++
		}
	}
}

func (b *ThompsonBandit) GetCandidateRate() float64 {
	if b.CandidatePulls == 0 {
		return 0
	}
	return b.CandidateReward / float64(b.CandidatePulls)
}

func (b *ThompsonBandit) GetBaselineRate() float64 {
	if b.BaselinePulls == 0 {
		return 0
	}
	return b.BaselineReward / float64(b.BaselinePulls)
}

func (b *ThompsonBandit) TotalPulls() int {
	return b.CandidatePulls + b.BaselinePulls
}

func betaSample(alpha, beta float64) float64 {
	x := gammaSample(alpha, 1.0)
	y := gammaSample(beta, 1.0)
	return x / (x + y)
}

func gammaSample(shape, rate float64) float64 {
	if shape < 1 {
		u := randFloat64()
		return gammaSample(shape+1, rate) * math.Pow(u, 1.0/shape)
	}

	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)

	for i := 0; i < 1000; i++ {
		x := randNormFloat64()
		v := 1.0 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := randFloat64()
		if u < 1.0-0.0331*(x*x)*(x*x) {
			return d * v / rate
		}
		if math.Log(u) < 0.5*x*x+d*(math.Log(v)-math.Log(d*v-1+1)) {
			return d * v / rate
		}
	}

	return d / rate
}

func randFloat64() float64 {
	globalRandMu.Lock()
	defer globalRandMu.Unlock()
	return globalRand.Float64()
}

func randNormFloat64() float64 {
	globalRandMu.Lock()
	defer globalRandMu.Unlock()
	return globalRand.NormFloat64()
}
