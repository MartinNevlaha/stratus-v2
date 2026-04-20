package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNoAPIKey             = errors.New("llm: no api key provided")
	ErrProviderNotSupported = errors.New("llm: provider not supported")
	ErrRequestFailed        = errors.New("llm: request failed")
	ErrRateLimited          = errors.New("llm: rate limited")
)

// RateLimitedError is returned when the provider responds with HTTP 429.
// RetryAfter carries the duration parsed from the Retry-After header (zero if absent).
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("llm: rate limited, retry after %v", e.RetryAfter)
	}
	return "llm: rate limited"
}

func (e *RateLimitedError) Is(target error) bool {
	return target == ErrRateLimited
}

// parseRetryAfter parses the Retry-After header per RFC 7231:
// it accepts both an integer seconds form and an HTTP-date form.
// Returns 0 if the header is absent or unparseable.
func parseRetryAfter(h http.Header) time.Duration {
	val := h.Get("Retry-After")
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	SystemPrompt string
	Messages     []Message
	MaxTokens    int
	Temperature  float64
	// ResponseFormat is an optional hint to force structured output. Valid values:
	//   - "" (empty) : provider default, no constraint
	//   - "json"     : request JSON-only output via the provider's native mechanism.
	// Providers that do not support JSON mode silently ignore the field; callers
	// should still parse defensively (see llm.ParseJSONResponse).
	ResponseFormat string
}

type CompletionResponse struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int
	FinishReason string
}

type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	Provider() string
	Model() string
}

func NewClient(cfg Config) (Client, error) {
	cfg = cfg.WithEnv()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var client Client
	var err error

	switch cfg.Provider {
	case "zai":
		client, err = NewZAIClient(cfg)
	case "openai":
		client, err = NewOpenAIClient(cfg)
	case "ollama":
		client, err = NewOllamaClient(cfg)
	case "anthropic":
		client, err = NewAnthropicClient(cfg)
	default:
		return nil, ErrProviderNotSupported
	}

	if err != nil {
		return nil, err
	}

	// Wrap retry first (inner), then semaphore (outer) so that a single semaphore
	// slot is held across all retry attempts — retry loops while holding the slot.
	if cfg.MaxRetries > 0 {
		client = NewClientWithRetry(client, RetryConfig{
			MaxRetries:  cfg.MaxRetries,
			InitialWait: 1 * time.Second,
			MaxWait:     30 * time.Second,
			Multiplier:  2.0,
		})
	}
	client = newSemaphoreClient(client, cfg)

	return client, nil
}

func MustNewClient(cfg Config) Client {
	client, err := NewClient(cfg)
	if err != nil {
		panic(err)
	}
	return client
}

// ParseJSONResponse extracts and unmarshals JSON from an LLM response. It handles:
// markdown code fences (```json ... ``` or ``` ... ```), bracket-depth balancing
// with string-state awareness, and single-object-to-slice auto-wrapping.
func ParseJSONResponse(response string, target interface{}) error {
	src := stripCodeFence(response)

	start := -1
	var opener byte
	for i := 0; i < len(src); i++ {
		if src[i] == '{' || src[i] == '[' {
			start = i
			opener = src[i]
			break
		}
	}
	if start == -1 {
		return errors.New("llm: no JSON found in response")
	}

	closer := byte('}')
	if opener == '[' {
		closer = ']'
	}

	depth := 0
	inString := false
	end := -1
	for i := start; i < len(src); i++ {
		c := src[i]
		if inString {
			if c == '\\' {
				i++ // skip escaped character
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c == opener {
			depth++
		} else if c == closer {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}

	if end == -1 {
		return errors.New("llm: unterminated JSON")
	}

	candidate := src[start : end+1]
	err := json.Unmarshal([]byte(candidate), target)
	if err == nil {
		return nil
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Value == "object" && opener == '{' {
		t := reflect.TypeOf(target)
		if t != nil && t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Slice {
			if err2 := json.Unmarshal([]byte("["+candidate+"]"), target); err2 == nil {
				return nil
			}
		}
	}

	// Lenient fallback: many local LLMs (gemma, llama) emit unquoted keys,
	// single-quoted strings, or trailing commas despite format:"json".
	// Apply common JSON5-style fixes and retry before giving up.
	if relaxed := relaxJSON(candidate); relaxed != candidate {
		if err2 := json.Unmarshal([]byte(relaxed), target); err2 == nil {
			return nil
		}
		// Also try the auto-wrap path on the relaxed form.
		if errors.As(err, &typeErr) && typeErr.Value == "object" && opener == '{' {
			t := reflect.TypeOf(target)
			if t != nil && t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Slice {
				if err3 := json.Unmarshal([]byte("["+relaxed+"]"), target); err3 == nil {
					return nil
				}
			}
		}
	}

	return err
}

// relaxJSON applies tolerant JSON fixes to an LLM-generated string:
//   - unquoted object keys ({foo: 1} → {"foo": 1})
//   - single-quoted strings ('x' → "x")
//   - trailing commas before } or ]
//
// It walks the input tracking string/escape state so transformations apply
// only to structural characters, never to content inside strings.
func relaxJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 32)

	inString := false
	var quoteChar byte

	for i := 0; i < len(s); i++ {
		c := s[i]

		if inString {
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(c)
				b.WriteByte(s[i+1])
				i++
				continue
			}
			if c == quoteChar {
				b.WriteByte('"')
				inString = false
				continue
			}
			if quoteChar == '\'' && c == '"' {
				b.WriteString(`\"`)
				continue
			}
			b.WriteByte(c)
			continue
		}

		if c == '"' || c == '\'' {
			b.WriteByte('"')
			inString = true
			quoteChar = c
			continue
		}

		if c == ',' {
			j := i + 1
			for j < len(s) && isJSONSpace(s[j]) {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // drop trailing comma
			}
		}

		b.WriteByte(c)

		if c == '{' || c == ',' {
			// Look ahead for a bare identifier followed by ':'. If present,
			// wrap it in quotes before continuing the outer loop.
			j := i + 1
			for j < len(s) && isJSONSpace(s[j]) {
				j++
			}
			if j < len(s) && isIdentStart(s[j]) {
				start := j
				for j < len(s) && isIdentCont(s[j]) {
					j++
				}
				k := j
				for k < len(s) && isJSONSpace(s[k]) {
					k++
				}
				if k < len(s) && s[k] == ':' {
					b.WriteString(s[i+1 : start])
					b.WriteByte('"')
					b.WriteString(s[start:j])
					b.WriteByte('"')
					i = j - 1
				}
			}
		}
	}

	return b.String()
}

func isJSONSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// stripCodeFence extracts content from the first markdown code fence if present.
// If no fence is found the input is returned unchanged.
func stripCodeFence(s string) string {
	open := strings.Index(s, "```")
	if open == -1 {
		return s
	}
	// skip past the opening ``` and optional language identifier + newline
	after := open + 3
	if nl := strings.Index(s[after:], "\n"); nl != -1 {
		after += nl + 1
	}
	close := strings.Index(s[after:], "```")
	if close == -1 {
		return s
	}
	return s[after : after+close]
}
