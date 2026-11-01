// Package llm talks to the language models people add to FloBase with their
// own API keys: Anthropic's Claude through its SDK, and any service with an
// OpenAI-compatible chat API (OpenAI, Gemini, OpenRouter, Groq, Mistral,
// DeepSeek, Ollama and others).
//
// A conversation's history is kept in the provider's own format, so nothing
// a model sends (such as Claude's thinking blocks) is lost between turns.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	KindAnthropic = "anthropic"
	KindOpenAI    = "openai"
)

// Provider is one model at one service, with the key to use it.
type Provider struct {
	Kind    string
	BaseURL string // empty for the service's default
	APIKey  string
	Model   string
}

// Tool is a function the model may call. Properties is a JSON Schema
// "properties" object.
type Tool struct {
	Name        string
	Description string
	Properties  map[string]any
	Required    []string
}

// Call is one tool call the model asked for.
type Call struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Result answers a Call.
type Result struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Reply is one model turn.
type Reply struct {
	Text  string
	Calls []Call
	// Stop is "end", "tool_use", "max_tokens" or "refusal".
	Stop string
}

// History is a conversation in the provider's own format. Turns are opaque
// JSON for this package; callers store and return them unchanged.
type History struct {
	Kind  string            `json:"kind"`
	Turns []json.RawMessage `json:"turns"`
}

// Error is a failure the person can act on, such as a rejected key.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// explain turns an HTTP status from a model service into a plain message.
func explain(status int, detail string) *Error {
	var msg string
	switch {
	case status == 401 || status == 403:
		msg = "The service rejected the API key."
	case status == 404:
		msg = "The service does not know this model or address. Check the model name and base URL."
	case status == 429:
		msg = "The service is rate limiting this key, or its credit has run out. Try again later."
	case status == 400:
		msg = "The service refused the request."
	case status >= 500:
		msg = "The model service had an error. Try again in a moment."
	default:
		msg = fmt.Sprintf("The model service answered with status %d.", status)
	}
	if detail != "" {
		msg += " (" + detail + ")"
	}
	return &Error{Status: status, Message: msg}
}

const requestTimeout = 5 * time.Minute

// Next sends the conversation and appends the model's reply to it.
func (p Provider) Next(ctx context.Context, system string, tools []Tool, h *History) (Reply, error) {
	switch p.Kind {
	case KindAnthropic:
		return p.anthropicNext(ctx, system, tools, h)
	case KindOpenAI:
		return p.openaiNext(ctx, system, tools, h)
	}
	return Reply{}, errors.New("unknown model kind " + p.Kind)
}

// AddUser appends a message from the person.
func AddUser(h *History, text string) {
	if h.Kind == KindAnthropic {
		addAnthropicUser(h, text)
	} else {
		addOpenAIUser(h, text)
	}
}

// AddResults appends the answers to the model's last tool calls, all in one
// turn.
func AddResults(h *History, results []Result) {
	if h.Kind == KindAnthropic {
		addAnthropicResults(h, results)
	} else {
		addOpenAIResults(h, results)
	}
}

// Models lists the models the key can use, for picking one.
func (p Provider) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if p.Kind == KindAnthropic {
		return p.anthropicModels(ctx)
	}
	return p.openaiModels(ctx)
}

// Test sends a short message to check the key and model work.
func (p Provider) Test(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	h := &History{Kind: p.Kind}
	AddUser(h, "Reply with the word OK.")
	_, err := p.Next(ctx, "You are checking that a connection works.", nil, h)
	return err
}
