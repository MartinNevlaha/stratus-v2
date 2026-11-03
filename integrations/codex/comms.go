package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MartinNevlaha/stratus-v2/hooks"
)

// Comms is best-effort: every helper swallows failures (after logging to stderr) so a
// down API never delays or breaks a Codex turn.

const commsTimeout = 700 * time.Millisecond

func commsPeerKey(sessionID string) string { return "codex:" + sessionID }

func newComms(base string) *hooks.CommsClient { return hooks.NewCommsClient(base, commsTimeout) }

// commsSessionNotice registers the session as a peer and returns its identity intro plus
// any waiting messages; "" when the API is unreachable.
func commsSessionNotice(c *hooks.CommsClient, sessionID, project string) string {
	self, err := c.RegisterPeer(commsPeerKey(sessionID), "codex", projectName(project))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Stratus Codex comms:", err)
		return ""
	}
	peers, err := c.ActivePeers()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Stratus Codex comms:", err)
	}
	notice := hooks.CommsIntro(self, peers, sessionID)
	if inbox := commsInbox(c, sessionID); inbox != "" {
		notice += "\n" + inbox
	}
	return notice
}

func projectName(root string) string {
	if root == "" {
		return ""
	}
	return filepath.Base(root)
}

// commsInbox claims the session's unread messages and formats them; "" when none or on error.
func commsInbox(c *hooks.CommsClient, sessionID string) string {
	msgs, err := c.ClaimInbox(commsPeerKey(sessionID))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Stratus Codex comms:", err)
		return ""
	}
	return hooks.FormatInbox(msgs)
}

func commsEnd(c *hooks.CommsClient, sessionID string) {
	if err := c.EndPeer(commsPeerKey(sessionID)); err != nil {
		fmt.Fprintln(os.Stderr, "Stratus Codex comms:", err)
	}
}
