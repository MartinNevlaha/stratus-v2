package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
)

// A broker owns only the transport. Claiming an input is irreversible: uncertain
// native sends require human reconciliation, never redelivery from this queue.
type teamAttachmentBroker struct {
	mu           sync.Mutex
	nativeID     string
	profile      teams.Profile
	generation   uint64
	pending      *attachmentPending
	observed     teams.NativeTurn
	receipts     map[string]attachmentReceipt
	receiptOrder []string
	wake         chan struct{}
	done         chan struct{}
	closed       bool
	lastSeen     time.Time
	lease        time.Duration
	onExpired    func()
}
type attachmentPending struct {
	input        teams.Input
	claimed      bool
	nativeTurnID string
	result       chan attachmentOutcome
}
type attachmentOutcome struct {
	result teams.Result
	err    error
}
type attachmentReceipt struct{ turn, text, failure string }

func newTeamAttachmentBroker(nativeID string, profile teams.Profile, lease time.Duration, expired func()) *teamAttachmentBroker {
	b := &teamAttachmentBroker{nativeID: nativeID, profile: profile, receipts: map[string]attachmentReceipt{}, wake: make(chan struct{}, 1), done: make(chan struct{}), lastSeen: time.Now(), lease: lease, onExpired: expired}
	go b.watchLease()
	return b
}
func (b *teamAttachmentBroker) watchLease() {
	interval := b.lease / 3
	if interval <= 0 {
		interval = time.Millisecond
	}
	timer := time.NewTicker(interval)
	defer timer.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-timer.C:
			b.mu.Lock()
			if !b.closed && time.Since(b.lastSeen) >= b.lease {
				callback := b.onExpired
				b.closeLocked(errors.New("Native transport lease expired; delivery is uncertain and will not be replayed"))
				b.mu.Unlock()
				if callback != nil {
					callback()
				}
				return
			}
			b.mu.Unlock()
		}
	}
}
func (b *teamAttachmentBroker) configure(generation uint64, expired func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.generation = generation
	b.onExpired = expired
}
func (b *teamAttachmentBroker) live(generation uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.closed && generation != 0 && b.generation == generation
}
func (b *teamAttachmentBroker) heartbeat() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return teams.ErrConflict
	}
	b.lastSeen = time.Now()
	return nil
}
func (b *teamAttachmentBroker) Submit(ctx context.Context, input teams.Input) (teams.Result, error) {
	if err := ctx.Err(); err != nil {
		return teams.Result{}, err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return teams.Result{}, teams.ErrClosed
	}
	if b.pending != nil || input.ID == "" {
		b.mu.Unlock()
		return teams.Result{}, teams.ErrConflict
	}
	if _, exists := b.receipts[input.ID]; exists {
		b.mu.Unlock()
		return teams.Result{}, teams.ErrConflict
	}
	p := &attachmentPending{input: input, result: make(chan attachmentOutcome, 1)}
	b.pending = p
	select {
	case b.wake <- struct{}{}:
	default:
	}
	b.mu.Unlock()
	select {
	case outcome := <-p.result:
		return outcome.result, outcome.err
	case <-ctx.Done():
		b.mu.Lock()
		b.closeLocked(fmt.Errorf("Native delivery cancelled; outcome uncertain: %w", ctx.Err()))
		b.mu.Unlock()
		return teams.Result{}, ctx.Err()
	}
}
func (b *teamAttachmentBroker) next(ctx context.Context) (*teams.Input, error) {
	if err := b.heartbeat(); err != nil {
		return nil, err
	}
	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return nil, teams.ErrConflict
		}
		if p := b.pending; p != nil && !p.claimed {
			p.claimed = true
			input := p.input
			b.mu.Unlock()
			return &input, nil
		}
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-b.done:
			return nil, teams.ErrConflict
		case <-b.wake:
		}
	}
}
func (b *teamAttachmentBroker) observe(turn teams.NativeTurn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.observed = turn
		b.lastSeen = time.Now()
		terminal := turn.State == "complete" || turn.State == "failed" || turn.State == "cancelled"
		if p := b.pending; terminal && p != nil && p.nativeTurnID != "" && p.nativeTurnID == turn.ID {
			p.result <- attachmentOutcome{err: errors.New("Native turn ended without an explicit team result; inspect the original chat before continuing")}
			b.pending = nil
		}
	}
}

// commit runs while the broker is locked so a result cannot race ahead of the
// durable manager acknowledgement. It must not call back into this broker.
func (b *teamAttachmentBroker) ack(taskID, turnID string, commit func() error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || turnID == "" {
		return teams.ErrConflict
	}
	if receipt, ok := b.receipts[taskID]; ok {
		if receipt.turn == turnID {
			return nil
		}
		return teams.ErrConflict
	}
	p := b.pending
	if p == nil || !p.claimed || p.input.ID != taskID {
		return teams.ErrConflict
	}
	if p.nativeTurnID != "" {
		if p.nativeTurnID == turnID {
			return nil
		}
		return teams.ErrConflict
	}
	if b.observed.ID != turnID || b.observed.State != "running" {
		return teams.ErrConflict
	}
	if err := commit(); err != nil {
		return err
	}
	p.nativeTurnID = turnID
	return nil
}
func (b *teamAttachmentBroker) acknowledged(taskID, turnID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	if p := b.pending; p != nil && p.input.ID == taskID && p.nativeTurnID == turnID && turnID != "" {
		return true
	}
	receipt, ok := b.receipts[taskID]
	return ok && receipt.turn == turnID && turnID != ""
}
func (b *teamAttachmentBroker) complete(taskID, turnID, text, failure string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return teams.ErrConflict
	}
	receipt := attachmentReceipt{turnID, text, failure}
	if old, ok := b.receipts[taskID]; ok {
		if old == receipt {
			return nil
		}
		return teams.ErrConflict
	}
	p := b.pending
	if p == nil || !p.claimed || p.input.ID != taskID || turnID == "" || p.nativeTurnID != turnID {
		return teams.ErrConflict
	}
	b.receipts[taskID] = receipt
	b.receiptOrder = append(b.receiptOrder, taskID)
	if len(b.receiptOrder) > 128 {
		delete(b.receipts, b.receiptOrder[0])
		b.receiptOrder = b.receiptOrder[1:]
	}
	outcome := attachmentOutcome{result: teams.Result{Text: text, NativeID: b.nativeID, Model: b.profile.Model, Effort: b.profile.Effort}}
	if failure != "" {
		outcome.err = errors.New(failure)
	}
	p.result <- outcome
	b.pending = nil
	return nil
}
func (b *teamAttachmentBroker) fail(taskID, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.pending == nil || b.pending.input.ID != taskID || !b.pending.claimed {
		return teams.ErrConflict
	}
	b.pending.result <- attachmentOutcome{err: fmt.Errorf("Native delivery uncertain; no automatic replay: %s", reason)}
	b.pending = nil
	return nil
}
func (b *teamAttachmentBroker) Approve(context.Context, string, bool) error {
	return errors.New("Native approvals remain in the original chat")
}
func (b *teamAttachmentBroker) Interrupt(context.Context) error {
	return errors.New("Attached transports do not interrupt native turns")
}
func (b *teamAttachmentBroker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeLocked(errors.New("Native transport detached; delivery outcome is uncertain"))
	return nil
}
func (b *teamAttachmentBroker) closeLocked(err error) {
	if b.closed {
		return
	}
	b.closed = true
	close(b.done)
	if b.pending != nil {
		b.pending.result <- attachmentOutcome{err: err}
		b.pending = nil
	}
}
