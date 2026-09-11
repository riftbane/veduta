package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func testServer() *Server {
	return &Server{
		Name: "veduta", Version: "test",
		Tools: []Tool{
			{Name: "echo", Description: "echo text", InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"additionalProperties":false}`),
				Handler: func(ctx context.Context, args json.RawMessage) (*Result, error) {
					var a struct {
						Text string `json:"text"`
					}
					if err := Strict(args, &a); err != nil {
						return nil, err
					}
					return &Result{Content: []Content{Text(a.Text), PNG([]byte{1, 2, 3})}}, nil
				}},
			{Name: "boom", Description: "fails", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
				Handler: func(ctx context.Context, args json.RawMessage) (*Result, error) { return nil, errors.New("kaput") }},
			{Name: "panic", Description: "panics", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
				Handler: func(ctx context.Context, args json.RawMessage) (*Result, error) { panic("oops") }},
		},
	}
}

func roundTrip(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var resps []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad response %q: %v", l, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func TestHandshakeAndTools(t *testing.T) {
	r := roundTrip(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":{"bogus":1}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"boom","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"panic"}}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":8,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"nope"}}`,
		`not json`,
	)
	if len(r) != 10 {
		t.Fatalf("got %d responses (notifications must not be answered): %v", len(r), r)
	}
	init := r[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "veduta" {
		t.Fatalf("initialize: %v", init)
	}
	if tools := r[1]["result"].(map[string]any)["tools"].([]any); len(tools) != 3 || tools[0].(map[string]any)["inputSchema"] == nil {
		t.Fatalf("tools/list: %v", tools)
	}
	echo := r[2]["result"].(map[string]any)
	content := echo["content"].([]any)
	if echo["isError"] != false || content[0].(map[string]any)["text"] != "hi" || content[1].(map[string]any)["mimeType"] != "image/png" || content[1].(map[string]any)["data"] != "AQID" {
		t.Fatalf("echo: %v", echo)
	}
	for _, i := range []int{3, 4, 5} {
		res := r[i]["result"].(map[string]any)
		if res["isError"] != true {
			t.Fatalf("response %d should be an error result: %v", i, r[i])
		}
	}
	if r[6]["result"] == nil || r[7]["error"].(map[string]any)["code"].(float64) != codeMethodNotFound {
		t.Fatalf("ping/unknown method: %v %v", r[6], r[7])
	}
	if r[8]["error"] == nil || r[9]["error"].(map[string]any)["code"].(float64) != codeParse {
		t.Fatalf("unknown tool / parse error: %v %v", r[8], r[9])
	}
}

func TestProtocolNegotiation(t *testing.T) {
	r := roundTrip(t, testServer(), `{"jsonrpc":"2.0","id":"a","method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":"b","method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if r[0]["result"].(map[string]any)["protocolVersion"] != "2024-11-05" || r[0]["id"] != "a" {
		t.Fatalf("old version not honoured: %v", r[0])
	}
	if r[1]["result"].(map[string]any)["protocolVersion"] != SupportedVersions[0] {
		t.Fatalf("unknown version should get the latest: %v", r[1])
	}
}

func TestBatch(t *testing.T) {
	var out bytes.Buffer
	s := testServer()
	s.Serve(context.Background(), strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`+"\n"), &out)
	var resps []map[string]any
	if err := json.Unmarshal(out.Bytes(), &resps); err != nil || len(resps) != 2 {
		t.Fatalf("batch: %s %v", out.String(), err)
	}
}
