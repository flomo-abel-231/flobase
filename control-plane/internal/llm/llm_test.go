package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A tool round trip against a stand-in Anthropic API: the second request must
// carry the first reply back unchanged, thinking block included, followed by
// the tool result.
func TestAnthropicToolRoundTrip(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" {
			w.WriteHeader(401)
			io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		requests = append(requests, body)
		w.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},
				"content":[{"type":"thinking","thinking":"","signature":"sig123"},{"type":"text","text":"Looking."},
				{"type":"tool_use","id":"tu_1","name":"list_projects","input":{}}]}`)
			return
		}
		io.WriteString(w, `{"id":"msg_2","type":"message","role":"assistant","model":"m","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1},
			"content":[{"type":"text","text":"You have one project."}]}`)
	}))
	defer srv.Close()

	p := Provider{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "key", Model: "m"}
	h := &History{Kind: KindAnthropic}
	AddUser(h, "What projects do I have?")
	tools := []Tool{{Name: "list_projects", Description: "List projects."}}

	reply, err := p.Next(context.Background(), "system", tools, h)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Stop != "tool_use" || len(reply.Calls) != 1 || reply.Calls[0].ID != "tu_1" || reply.Text != "Looking." {
		t.Fatalf("first reply = %+v", reply)
	}
	AddResults(h, []Result{{CallID: "tu_1", Content: `[{"name":"shop"}]`}})

	// The history survives being stored as JSON.
	raw, _ := json.Marshal(h)
	var stored History
	json.Unmarshal(raw, &stored)

	reply, err = p.Next(context.Background(), "system", tools, &stored)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Stop != "end" || reply.Text != "You have one project." {
		t.Fatalf("second reply = %+v", reply)
	}
	sent, _ := json.Marshal(requests[1]["messages"])
	for _, want := range []string{`"signature":"sig123"`, `"type":"tool_use"`, `"tool_use_id":"tu_1"`, `"type":"tool_result"`} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("second request lacks %s: %s", want, sent)
		}
	}

	bad := p
	bad.APIKey = "wrong"
	if err := bad.Test(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Errorf("bad key error = %v", err)
	}
}

func TestOpenAIToolRoundTrip(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"b-model"},{"id":"a-model"}]}`)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		requests = append(requests, body)
		if len(requests) == 1 {
			io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"x",
				"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_project","arguments":"{\"ref\":\"abc\"}"}}]}}]}`)
			return
		}
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"It is active."}}]}`)
	}))
	defer srv.Close()

	p := Provider{Kind: KindOpenAI, BaseURL: srv.URL + "/v1", Model: "a-model"}
	models, err := p.Models(context.Background())
	if err != nil || strings.Join(models, ",") != "a-model,b-model" {
		t.Fatalf("models = %v, %v", models, err)
	}
	h := &History{Kind: KindOpenAI}
	AddUser(h, "Is abc up?")
	reply, err := p.Next(context.Background(), "system", []Tool{{Name: "get_project", Properties: map[string]any{"ref": map[string]any{"type": "string"}}, Required: []string{"ref"}}}, h)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Calls) != 1 || string(reply.Calls[0].Input) != `{"ref":"abc"}` {
		t.Fatalf("reply = %+v", reply)
	}
	AddResults(h, []Result{{CallID: "call_1", Content: `{"status":"active"}`}})
	reply, err = p.Next(context.Background(), "system", nil, h)
	if err != nil || reply.Text != "It is active." {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	sent, _ := json.Marshal(requests[1]["messages"])
	if strings.Contains(string(sent), "reasoning_content") || !strings.Contains(string(sent), `"tool_call_id":"call_1"`) {
		t.Errorf("second request = %s", sent)
	}
	if _, ok := requests[1]["tools"]; ok {
		t.Error("tools sent when there were none")
	}
}
