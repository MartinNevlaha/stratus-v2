package hooks

import (
	"path/filepath"
	"strings"
)

type DeliveryPolicyInput struct {
	Role, Phase, Command, ProjectRoot string
	WritePaths                        []string
	Delegation, WorkflowResolved      bool
}

// EvaluateDeliveryPolicy reuses pure policy checks; runtime adapters retain their own I/O.
func EvaluateDeliveryPolicy(in DeliveryPolicyInput) Decision {
	allow := Decision{Continue: true}
	if !strings.HasPrefix(in.Role, "delivery-") {
		return allow
	}
	writes := len(in.WritePaths) > 0 || in.Command != "" && isWriteBashCommand(in.Command)
	if in.Delegation && !in.WorkflowResolved || writes && !in.WorkflowResolved {
		return Decision{Reason: "No unambiguous owned workflow. Register or explicitly bind the workflow first."}
	}
	if docsOnlyAgents[in.Role] {
		if in.Command != "" && isWriteBashCommand(in.Command) && !isDocsOnlyBashWrite(in.Command, in.ProjectRoot) {
			return Decision{Reason: "Architects may write only Markdown under docs/"}
		}
		for _, p := range in.WritePaths {
			if !filepath.IsAbs(p) {
				p = filepath.Join(in.ProjectRoot, p)
			}
			if !isProjectDoc(p, in.ProjectRoot) {
				return Decision{Reason: "Architects may write only Markdown under docs/"}
			}
		}
	}
	if writes && (in.Phase == "verify" || in.Phase == "review") {
		return Decision{Reason: "Writes are not allowed during " + in.Phase + "; use read-only tools or return to implementation."}
	}
	return allow
}
