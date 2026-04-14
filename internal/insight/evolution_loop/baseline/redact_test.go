package baseline_test

import (
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

func TestRedact_NilBundle_ReturnsNil(t *testing.T) {
	got := baseline.Redact(nil)
	if got != nil {
		t.Errorf("expected nil, got non-nil bundle")
	}
}

func TestRedact_EmptyBundle_NoP(t *testing.T) {
	b := &baseline.Bundle{}
	got := baseline.Redact(b)
	if got == nil {
		t.Fatal("expected non-nil bundle")
	}
	if len(got.VexorHits) != 0 {
		t.Errorf("expected empty VexorHits")
	}
	if len(got.TODOs) != 0 {
		t.Errorf("expected empty TODOs")
	}
	if len(got.GitCommits) != 0 {
		t.Errorf("expected empty GitCommits")
	}
}

func TestRedact_AWSAccessKey_Redacted(t *testing.T) {
	b := &baseline.Bundle{
		VexorHits: []baseline.VexorHit{
			{Snippet: "config: AKIAIOSFODNN7EXAMPLE123 is the key"},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.VexorHits[0].Snippet, "AKIAIOSFODNN7EXAMPLE123") {
		t.Errorf("AWS access key should be redacted, got: %s", got.VexorHits[0].Snippet)
	}
	if !strings.Contains(got.VexorHits[0].Snippet, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker in snippet: %s", got.VexorHits[0].Snippet)
	}
}

func TestRedact_BearerToken_Redacted(t *testing.T) {
	b := &baseline.Bundle{
		TODOs: []baseline.TODOItem{
			{Text: `Authorization: bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9abcdef`},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.TODOs[0].Text, "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9abcdef") {
		t.Errorf("bearer token should be redacted, got: %s", got.TODOs[0].Text)
	}
	if !strings.Contains(got.TODOs[0].Text, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in text: %s", got.TODOs[0].Text)
	}
}

func TestRedact_PEMBlock_LineRedacted(t *testing.T) {
	b := &baseline.Bundle{
		VexorHits: []baseline.VexorHit{
			{Snippet: "-----BEGIN RSA PRIVATE KEY-----\nsome_key_data\n-----END RSA PRIVATE KEY-----"},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.VexorHits[0].Snippet, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("PEM block line should be redacted, got: %s", got.VexorHits[0].Snippet)
	}
	if !strings.Contains(got.VexorHits[0].Snippet, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker: %s", got.VexorHits[0].Snippet)
	}
}

func TestRedact_EnvStyleAssignment_RHSRedacted(t *testing.T) {
	b := &baseline.Bundle{
		VexorHits: []baseline.VexorHit{
			{Snippet: "DATABASE_PASSWORD=supersecretlongpassword123\nother=stuff"},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.VexorHits[0].Snippet, "supersecretlongpassword123") {
		t.Errorf("env value should be redacted, got: %s", got.VexorHits[0].Snippet)
	}
	// LHS should still be present
	if !strings.Contains(got.VexorHits[0].Snippet, "DATABASE_PASSWORD") {
		t.Errorf("env LHS (DATABASE_PASSWORD) should be preserved, got: %s", got.VexorHits[0].Snippet)
	}
	if !strings.Contains(got.VexorHits[0].Snippet, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker: %s", got.VexorHits[0].Snippet)
	}
}

func TestRedact_GitHubToken_Redacted(t *testing.T) {
	b := &baseline.Bundle{
		GitCommits: []baseline.GitCommit{
			{Subject: "add token ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij to config"},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.GitCommits[0].Subject, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij") {
		t.Errorf("GitHub token should be redacted, got: %s", got.GitCommits[0].Subject)
	}
	if !strings.Contains(got.GitCommits[0].Subject, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in subject: %s", got.GitCommits[0].Subject)
	}
}

func TestRedact_GitLabToken_Redacted(t *testing.T) {
	b := &baseline.Bundle{
		VexorHits: []baseline.VexorHit{
			{Snippet: "token: glpat-xyzABC123_some-more-chars-here"},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.VexorHits[0].Snippet, "glpat-xyzABC123_some-more-chars-here") {
		t.Errorf("GitLab token should be redacted, got: %s", got.VexorHits[0].Snippet)
	}
	if !strings.Contains(got.VexorHits[0].Snippet, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in snippet: %s", got.VexorHits[0].Snippet)
	}
}

func TestRedact_NonSecretContent_Untouched(t *testing.T) {
	original := "TODO: refactor parser to use AST walker instead of regex"
	b := &baseline.Bundle{
		TODOs: []baseline.TODOItem{
			{Text: original},
		},
	}
	got := baseline.Redact(b)
	if got.TODOs[0].Text != original {
		t.Errorf("non-secret content should be untouched, got: %q", got.TODOs[0].Text)
	}
}

func TestRedact_MultipleSecretsInOneSnippet_AllRedacted(t *testing.T) {
	b := &baseline.Bundle{
		VexorHits: []baseline.VexorHit{
			{Snippet: "key=AKIAIOSFODNN7EXAMPLE456\ntoken=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.VexorHits[0].Snippet, "AKIAIOSFODNN7EXAMPLE456") {
		t.Errorf("AWS key should be redacted")
	}
	if strings.Contains(got.VexorHits[0].Snippet, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij") {
		t.Errorf("GitHub token should be redacted")
	}
}

func TestRedact_APIKeyPattern_Redacted(t *testing.T) {
	b := &baseline.Bundle{
		VexorHits: []baseline.VexorHit{
			{Snippet: `api_key = "sk-abcdefghijklmnopqrstuvwxyz1234567890abcdef"`},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.VexorHits[0].Snippet, "sk-abcdefghijklmnopqrstuvwxyz1234567890abcdef") {
		t.Errorf("api_key value should be redacted, got: %s", got.VexorHits[0].Snippet)
	}
}

func TestRedact_GCPServiceAccount_Redacted(t *testing.T) {
	b := &baseline.Bundle{
		VexorHits: []baseline.VexorHit{
			{Snippet: `"private_key": "-----BEGIN PRIVATE KEY-----\nABCD\n-----END PRIVATE KEY-----"`},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.VexorHits[0].Snippet, `"private_key": "-----BEGIN`) {
		t.Errorf("GCP private_key should be redacted, got: %s", got.VexorHits[0].Snippet)
	}
}

func TestRedact_AllFieldsRedacted_VexorTODOCommit(t *testing.T) {
	b := &baseline.Bundle{
		VexorHits: []baseline.VexorHit{
			{Snippet: "secret = supersecrettokenvalue12345abcdef"},
		},
		TODOs: []baseline.TODOItem{
			{Text: "password: myverysecretpassword123456789"},
		},
		GitCommits: []baseline.GitCommit{
			{Subject: "update token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"},
		},
	}
	got := baseline.Redact(b)
	if strings.Contains(got.VexorHits[0].Snippet, "supersecrettokenvalue12345abcdef") {
		t.Errorf("VexorHit secret should be redacted")
	}
	if strings.Contains(got.GitCommits[0].Subject, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij") {
		t.Errorf("commit token should be redacted")
	}
}
