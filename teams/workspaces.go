package teams

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// ErrWorkspaceCreate reports that a member's worktree could not be created. The provider's
// detail (which may name absolute paths) goes to the server log, never into this error.
var ErrWorkspaceCreate = errors.New("workspace creation failed; see the server log")

// BlockedError is a removal the provider refused because work would be lost.
type BlockedError struct{ Blockers []string }

func (e *BlockedError) Error() string {
	return strings.Join(e.Blockers, "; ") + " (pass force=true to discard)"
}
func (e *BlockedError) Unwrap() error { return ErrConflict }

// IntegrationConflict lists the files a member branch could not merge cleanly.
type IntegrationConflict struct {
	Member string   `json:"member"`
	Branch string   `json:"branch"`
	Files  []string `json:"files"`
	Reason string   `json:"reason,omitempty"`
}

// IntegrationResult describes one pass of merging member branches into the
// integration worktree. Branches are named after the members they belong to.
type IntegrationResult struct {
	Dir            string                `json:"-"`
	Branch         string                `json:"branch"`
	Base           string                `json:"-"`
	Revision       string                `json:"revision"`
	Dirty          bool                  `json:"dirty"`
	Merged         []string              `json:"merged"`
	Conflicts      []IntegrationConflict `json:"conflicts"`
	Failed         []IntegrationConflict `json:"failed"` // merges that failed for a reason other than a conflict
	NothingToMerge []string              `json:"nothing_to_merge"`
	Warnings       []string              `json:"warnings"`
}

// WorkspaceProvider gives write-capable members of an isolated team their own
// checkout and merges their results. Implementations must not touch anything
// outside <root>/.stratus/worktrees and the team's own branches.
type WorkspaceProvider interface {
	// Create makes a worktree and branch for one member from the project's current HEAD.
	// It leaves nothing behind when it fails.
	Create(ctx context.Context, projectRoot, teamID, memberID string) (dir, branch string, err error)
	// Integrate creates or reuses the integration worktree and merges every member
	// branch that is ahead of it. A conflicting merge is aborted and reported.
	Integrate(ctx context.Context, team Team) (IntegrationResult, error)
	// Remove deletes the team's worktrees and branches. Without force it refuses
	// (wrapping ErrConflict) when one holds unmerged commits or uncommitted changes.
	Remove(ctx context.Context, team Team, force bool) error
}

// workspaceIDPattern keeps member IDs usable inside branch names and directory names.
var workspaceIDPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]{0,46}[A-Za-z0-9_])?$`)

// ValidWorkspaceID reports whether a member ID may name a worktree. "integration"
// is reserved for the team's merge worktree.
func ValidWorkspaceID(id string) bool {
	return !strings.EqualFold(id, "integration") && workspaceIDPattern.MatchString(id)
}

// WorkspaceRole reports whether a role writes code and so gets its own worktree.
func WorkspaceRole(role string) bool {
	c := CanonicalRole(role)
	return c == "implementation" || c == "mechanical"
}

// MemberRoot is the directory a member's session runs in: its own worktree for
// write-capable members, the integration worktree (once it exists) for reviewers,
// the project root otherwise. Attached native chats always share the root.
func (t *Team) MemberRoot(m *Member) string {
	if t.IsolatedWorkspaces && m.Attachment == nil {
		if m.WorkspaceDir != "" && WorkspaceRole(m.Role) {
			return m.WorkspaceDir
		}
		if CanonicalRole(m.Role) == "review" && t.IntegrationDir != "" {
			return t.IntegrationDir
		}
	}
	return t.ProjectRoot
}

// ReviewRoot is the tree a review of this team's work describes.
func (t *Team) ReviewRoot() string {
	if t.IsolatedWorkspaces && t.IntegrationDir != "" {
		return t.IntegrationDir
	}
	return t.ProjectRoot
}

// SetWorkspaceProvider enables isolated workspaces for newly created teams.
func (m *Manager) SetWorkspaceProvider(p WorkspaceProvider) {
	m.mu.Lock()
	m.workspaces = p
	m.mu.Unlock()
}

// provisionWorkspaces validates an isolated request and creates a worktree for each
// write-capable member. The returned cleanup removes whatever was created; it is a
// no-op for teams that are not isolated.
func (m *Manager) provisionWorkspaces(ctx context.Context, tm *Team, attached bool) (cleanup func(), err error) {
	cleanup = func() {}
	if !tm.IsolatedWorkspaces {
		return cleanup, nil
	}
	m.mu.Lock()
	provider := m.workspaces
	m.mu.Unlock()
	if provider == nil {
		return cleanup, errors.New("isolated workspaces are unavailable")
	}
	if attached {
		return cleanup, errors.New("isolated workspaces cannot be combined with an attached native chat")
	}
	names := map[string]bool{}
	for _, member := range tm.Members {
		if !WorkspaceRole(member.Role) {
			continue
		}
		if !ValidWorkspaceID(member.ID) {
			return cleanup, fmt.Errorf("member ID %q cannot name a workspace: use 1-48 letters, digits, '_' or '-', starting with a letter or digit and not ending in '-'", member.ID)
		}
		// Case-insensitive file systems would map these onto one directory.
		lower := strings.ToLower(member.ID)
		if names[lower] {
			return cleanup, fmt.Errorf("member IDs %q collide when compared without case", member.ID)
		}
		names[lower] = true
	}
	cleanup = func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Nothing was committed to these worktrees, so removal is forced.
		_ = provider.Remove(rmCtx, *tm, true)
	}
	for i := range tm.Members {
		member := &tm.Members[i]
		if !WorkspaceRole(member.Role) {
			continue
		}
		member.WorkspaceDir, member.WorkspaceBranch, err = provider.Create(ctx, tm.ProjectRoot, tm.ID, member.ID)
		if err != nil {
			member.WorkspaceDir, member.WorkspaceBranch = "", ""
			cleanup()
			log.Printf("teams: create workspace for member %s: %v", member.ID, err)
			return func() {}, fmt.Errorf("%w: member %s", ErrWorkspaceCreate, member.ID)
		}
	}
	return cleanup, nil
}

// ensureWorkspace gives a member created after team creation (a worker added on
// demand) its worktree before its first turn, so it never writes into the shared root.
func (m *Manager) ensureWorkspace(slotCtx context.Context, teamID, memberID string) error {
	m.mu.Lock()
	rec := m.teams[teamID]
	if rec == nil || !rec.team.IsolatedWorkspaces {
		m.mu.Unlock()
		return nil
	}
	member := findMember(rec.team, memberID)
	if member == nil || member.Attachment != nil || !WorkspaceRole(member.Role) || member.WorkspaceDir != "" {
		m.mu.Unlock()
		return nil
	}
	provider, root := m.workspaces, rec.team.ProjectRoot
	m.mu.Unlock()
	if provider == nil {
		return errors.New("isolated workspaces are unavailable")
	}
	if !ValidWorkspaceID(memberID) {
		return fmt.Errorf("member ID %q cannot name a workspace", memberID)
	}
	ctx, cancel := context.WithTimeout(slotCtx, 2*time.Minute)
	defer cancel()
	dir, branch, err := provider.Create(ctx, root, teamID, memberID)
	if err != nil {
		log.Printf("teams: create workspace for member %s: %v", memberID, err)
		return fmt.Errorf("%w: member %s", ErrWorkspaceCreate, memberID)
	}
	// Whatever follows must not leave the new worktree behind: nothing records it yet.
	discard := func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stub := Team{ID: teamID, ProjectRoot: root, Members: []Member{{ID: memberID, WorkspaceDir: dir, WorkspaceBranch: branch}}}
		if rmErr := provider.Remove(rmCtx, stub, true); rmErr != nil {
			log.Printf("teams: remove unused workspace of member %s: %v", memberID, rmErr)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	member = findMember(rec.team, memberID)
	if member == nil {
		discard()
		return ErrNotFound
	}
	before := cloneTeam(rec.team)
	member.WorkspaceDir, member.WorkspaceBranch = dir, branch
	if err = m.saveLocked(rec); err != nil {
		rec.team = before
		discard()
		return fmt.Errorf("persist workspace for member %s: %w", memberID, err)
	}
	return nil
}

// beginWorkspaceOp reserves the team for one integration or removal at a time and
// returns a snapshot to hand the provider outside the lock.
func (m *Manager) beginWorkspaceOp(id string, mutating bool) (Team, WorkspaceProvider, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Team{}, nil, nil, ErrClosed
	}
	rec := m.teams[id]
	if rec == nil {
		return Team{}, nil, nil, ErrNotFound
	}
	if !rec.team.IsolatedWorkspaces {
		return Team{}, nil, nil, fmt.Errorf("%w: this team does not use isolated workspaces", ErrConflict)
	}
	if m.workspaces == nil {
		return Team{}, nil, nil, errors.New("isolated workspaces are unavailable")
	}
	if m.workspaceBusy[id] {
		return Team{}, nil, nil, fmt.Errorf("%w: another workspace operation is running", ErrConflict)
	}
	if mutating {
		for _, member := range rec.team.Members {
			s := m.slots[slotKey(id, member.ID)]
			// A member that waits for the user keeps its session, and so its working
			// directory, until it is answered.
			parked := (member.Approval != nil || member.State == "needs_user" || member.State == "recovering") &&
				s != nil && s.session != nil && s.root != "" && s.root != rec.team.ProjectRoot
			if member.State == "working" || (s != nil && s.busy) || parked {
				return Team{}, nil, nil, fmt.Errorf("%w: member %s is working or waiting for an answer; resolve it first", ErrConflict, member.ID)
			}
		}
	}
	if m.workspaceBusy == nil {
		m.workspaceBusy = map[string]bool{}
	}
	m.workspaceBusy[id] = true
	release := func() {
		m.mu.Lock()
		delete(m.workspaceBusy, id)
		m.signalLocked() // work queued meanwhile may run now
		m.mu.Unlock()
	}
	return *cloneTeam(rec.team), m.workspaces, release, nil
}

// Integrate merges the members' committed work into the team's integration
// worktree and records where it lives, so reviews describe that exact tree.
func (m *Manager) Integrate(ctx context.Context, id string) (IntegrationResult, error) {
	snapshot, provider, release, err := m.beginWorkspaceOp(id, false)
	if err != nil {
		return IntegrationResult{}, err
	}
	defer release()
	res, err := provider.Integrate(ctx, snapshot)
	if err != nil {
		return res, fmt.Errorf("integrate workspaces: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.teams[id]
	if rec == nil {
		return res, ErrNotFound
	}
	if rec.team.IntegrationDir != res.Dir || rec.team.IntegrationBranch != res.Branch || rec.team.IntegrationBase != res.Base {
		before := cloneTeam(rec.team)
		rec.team.IntegrationDir, rec.team.IntegrationBranch, rec.team.IntegrationBase = res.Dir, res.Branch, res.Base
		if err = m.saveLocked(rec); err != nil {
			rec.team = before
			return res, fmt.Errorf("persist integration workspace: %w", err)
		}
		m.signalLocked()
	}
	return res, nil
}

// RemoveWorkspaces deletes the team's worktrees and branches and ends isolation,
// so later turns run in the project root. Without force the provider refuses to
// discard unmerged or uncommitted work.
func (m *Manager) RemoveWorkspaces(ctx context.Context, id string, force bool) (*Team, error) {
	snapshot, provider, release, err := m.beginWorkspaceOp(id, true)
	if err != nil {
		return nil, err
	}
	defer release()
	if err = provider.Remove(ctx, snapshot, force); err != nil {
		return nil, fmt.Errorf("remove workspaces: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.teams[id]
	if rec == nil {
		return nil, ErrNotFound
	}
	before := cloneTeam(rec.team)
	for i := range rec.team.Members {
		rec.team.Members[i].WorkspaceDir, rec.team.Members[i].WorkspaceBranch = "", ""
	}
	rec.team.IsolatedWorkspaces = false
	rec.team.IntegrationDir, rec.team.IntegrationBranch, rec.team.IntegrationBase = "", "", ""
	if err = m.saveLocked(rec); err != nil {
		rec.team = before
		return nil, fmt.Errorf("persist workspace removal: %w", err)
	}
	m.signalLocked()
	return cloneTeam(rec.team), nil
}

// resumableNativeID is the native session a member's next session may resume. A native
// session belongs to the working directory it was opened in. Verified in teamruntime:
// OpenCode refuses to resume a session whose directory differs ("session belongs to
// another working directory", opencode.go) and so would fail the turn; Claude Code
// keeps sessions per project directory, so `--resume` from another cwd cannot be
// relied on to find the history; Codex thread/resume passes the new cwd but promises
// no history continuity across it. When the directory changed (a reviewer moving to the
// integration worktree, or a worktree that was removed), start a fresh session instead.
func resumableNativeID(tm *Team, m *Member) string {
	opened := m.SessionRoot
	if opened == "" {
		opened = tm.ProjectRoot
	}
	if m.NativeID != "" && opened != tm.MemberRoot(m) {
		return ""
	}
	return m.NativeID
}
