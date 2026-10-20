package hooks

import (
	"path/filepath"
	"testing"
)

func TestDeliveryPolicy_BridgeParity(t *testing.T) {
	root := t.TempDir()
	for _, c := range []struct {
		in   DeliveryPolicyInput
		want bool
	}{
		{DeliveryPolicyInput{Role: "delivery-backend-engineer", Phase: "verify", WritePaths: []string{"x.go"}, WorkflowResolved: true}, false},
		{DeliveryPolicyInput{Role: "delivery-backend-engineer", Phase: "verify", Command: "git diff", WorkflowResolved: true}, true},
		{DeliveryPolicyInput{Role: "delivery-system-architect", Phase: "design", ProjectRoot: root, WritePaths: []string{filepath.Join(root, "docs/a.md")}, WorkflowResolved: true}, true},
		{DeliveryPolicyInput{Role: "delivery-system-architect", Phase: "design", ProjectRoot: root, WritePaths: []string{"x.go"}, WorkflowResolved: true}, false},
		{DeliveryPolicyInput{Role: "explorer", WritePaths: []string{"x.go"}}, true},
		{DeliveryPolicyInput{Role: "delivery-backend-engineer", Delegation: true}, false},
	} {
		if d := EvaluateDeliveryPolicy(c.in); d.Continue != c.want {
			t.Fatal(c.in, d)
		}
	}
}
