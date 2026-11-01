package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
)

// Services with an OpenAI-compatible chat API. History turns are chat
// messages as the API takes them, without the system message, which is
// added on every request.

const openaiDefaultURL = "https://api.openai.com/v1"

var httpClient = &http.Client{Timeout: requestTimeout}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (p Provider) openaiURL(path string) string {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = openaiDefaultURL
	}
	return base + path
}

func addOpenAIUser(h *History, text string) {
	h.Turns = append(h.Turns, mustJSON(openaiMessage{Role: "user", Content: &text}))
}

func addOpenAIResults(h *History, results []Result) {
	for _, r := range results {
		content := r.Content
		if r.IsError {
			content = "Error: " + content
		}
		h.Turns = append(h.Turns, mustJSON(openaiMessage{Role: "tool", Content: &content, ToolCallID: r.CallID}))
	}
}

func (p Provider) openaiDo(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.openaiURL(path), reader)
	if err != nil {
		return nil, &Error{Message: "The base URL is not valid."}
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{Message: "The model took too long to answer."}
		}
		return nil, &Error{Message: "Could not reach the model service: " + err.Error()}
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(out, &e)
		return nil, explain(resp.StatusCode, e.Error.Message)
	}
	return out, nil
}

func (p Provider) openaiNext(ctx context.Context, system string, tools []Tool, h *History) (Reply, error) {
	messages := []json.RawMessage{mustJSON(openaiMessage{Role: "system", Content: &system})}
	messages = append(messages, h.Turns...)
	req := map[string]any{"model": p.Model, "messages": messages}
	if len(tools) > 0 {
		defs := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			required := t.Required
			if required == nil {
				required = []string{}
			}
			defs = append(defs, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  map[string]any{"type": "object", "properties": t.Properties, "required": required},
				},
			})
		}
		req["tools"] = defs
	}
	raw, err := p.openaiDo(ctx, http.MethodPost, "/chat/completions", req)
	if err != nil {
		return Reply{}, err
	}
	var resp struct {
		Choices []struct {
			Message      openaiMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Choices) == 0 {
		return Reply{}, &Error{Message: "The model service sent an answer FloBase could not read."}
	}
	choice := resp.Choices[0]
	// Keep only the fields every compatible service accepts back.
	msg := openaiMessage{Role: "assistant", Content: choice.Message.Content, ToolCalls: choice.Message.ToolCalls}
	for i := range msg.ToolCalls {
		msg.ToolCalls[i].Type = "function"
	}
	h.Turns = append(h.Turns, mustJSON(msg))

	reply := Reply{Stop: "end"}
	if msg.Content != nil {
		reply.Text = *msg.Content
	}
	for _, c := range msg.ToolCalls {
		args := c.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		reply.Calls = append(reply.Calls, Call{ID: c.ID, Name: c.Function.Name, Input: json.RawMessage(args)})
	}
	switch {
	case len(reply.Calls) > 0:
		reply.Stop = "tool_use"
	case choice.FinishReason == "length":
		reply.Stop = "max_tokens"
	case choice.FinishReason == "content_filter":
		reply.Stop = "refusal"
	}
	return reply, nil
}

func (p Provider) openaiModels(ctx context.Context) ([]string, error) {
	raw, err := p.openaiDo(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, &Error{Message: "The model service sent a model list FloBase could not read."}
	}
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		ids = append(ids, strings.TrimPrefix(m.ID, "models/"))
	}
	sort.Strings(ids)
	return ids, nil
}
