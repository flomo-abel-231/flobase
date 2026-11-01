package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Claude through the Anthropic SDK. History turns are one of:
//
//	{"role":"user","text":"..."}
//	{"role":"user","results":[...]}
//	{"role":"assistant","message":<the API's response, unchanged>}
//
// Keeping the whole response means thinking blocks go back as they came.
type anthropicTurn struct {
	Role    string          `json:"role"`
	Text    string          `json:"text,omitempty"`
	Results []Result        `json:"results,omitempty"`
	Message json.RawMessage `json:"message,omitempty"`
}

func (p Provider) anthropicClient() anthropic.Client {
	opts := []option.RequestOption{
		option.WithAPIKey(p.APIKey),
		option.WithMaxRetries(2),
		option.WithRequestTimeout(requestTimeout),
	}
	if p.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(p.BaseURL))
	}
	return anthropic.NewClient(opts...)
}

func addAnthropicUser(h *History, text string) {
	raw, _ := json.Marshal(anthropicTurn{Role: "user", Text: text})
	h.Turns = append(h.Turns, raw)
}

func addAnthropicResults(h *History, results []Result) {
	raw, _ := json.Marshal(anthropicTurn{Role: "user", Results: results})
	h.Turns = append(h.Turns, raw)
}

func anthropicMessages(h *History) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(h.Turns))
	for _, raw := range h.Turns {
		var t anthropicTurn
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		switch {
		case t.Role == "assistant":
			var m anthropic.Message
			if err := json.Unmarshal(t.Message, &m); err != nil {
				return nil, err
			}
			out = append(out, m.ToParam())
		case len(t.Results) > 0:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(t.Results))
			for _, r := range t.Results {
				blocks = append(blocks, anthropic.NewToolResultBlock(r.CallID, r.Content, r.IsError))
			}
			out = append(out, anthropic.NewUserMessage(blocks...))
		default:
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(t.Text)))
		}
	}
	return out, nil
}

func (p Provider) anthropicNext(ctx context.Context, system string, tools []Tool, h *History) (Reply, error) {
	messages, err := anthropicMessages(h)
	if err != nil {
		return Reply{}, err
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.Model),
		MaxTokens: 16000,
		Messages:  messages,
		// The system prompt and tools are the same on every turn: cache them.
		System: []anthropic.TextBlockParam{{Text: system, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
	}
	for _, t := range tools {
		tool := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: t.Properties, Required: t.Required},
		}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &tool})
	}
	client := p.anthropicClient()
	resp, err := client.Messages.New(ctx, params)
	if err != nil {
		return Reply{}, anthropicError(err)
	}
	h.Turns = append(h.Turns, mustJSON(anthropicTurn{Role: "assistant", Message: json.RawMessage(resp.RawJSON())}))

	var reply Reply
	var text []string
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text = append(text, b.Text)
		case anthropic.ToolUseBlock:
			reply.Calls = append(reply.Calls, Call{ID: b.ID, Name: b.Name, Input: json.RawMessage(b.JSON.Input.Raw())})
		}
	}
	reply.Text = strings.Join(text, "\n\n")
	switch resp.StopReason {
	case anthropic.StopReasonToolUse:
		reply.Stop = "tool_use"
	case anthropic.StopReasonMaxTokens:
		reply.Stop = "max_tokens"
	case anthropic.StopReasonRefusal:
		reply.Stop = "refusal"
	default:
		reply.Stop = "end"
	}
	return reply, nil
}

func anthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		detail := ""
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(apiErr.RawJSON()), &body) == nil {
			detail = body.Error.Message
		}
		return explain(apiErr.StatusCode, detail)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Message: "The model took too long to answer."}
	}
	return &Error{Message: "Could not reach the model service: " + err.Error()}
}

func (p Provider) anthropicModels(ctx context.Context) ([]string, error) {
	client := p.anthropicClient()
	pager := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	var ids []string
	for pager.Next() {
		ids = append(ids, pager.Current().ID)
	}
	if err := pager.Err(); err != nil {
		return nil, anthropicError(err)
	}
	return ids, nil
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
