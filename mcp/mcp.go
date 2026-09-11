// Package mcp is a minimal Model Context Protocol server: JSON-RPC 2.0 over stdio,
// newline-delimited, with the tools capability (tools/list, tools/call), initialize,
// notifications/initialized and ping. Logs go to the configured writer (stderr), never to
// the protocol stream. Tool failures are results with isError set, not transport errors.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// SupportedVersions are the protocol versions the server speaks, newest first.
var SupportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Content is one content block of a tool result.
type Content struct {
	Type     string `json:"type"`               // "text" or "image"
	Text     string `json:"text,omitempty"`     // text blocks
	Data     string `json:"data,omitempty"`     // image blocks: base64
	MimeType string `json:"mimeType,omitempty"` // image blocks: image/png
}

// Text returns a text block.
func Text(s string) Content { return Content{Type: "text", Text: s} }

// PNG returns an image block holding PNG bytes.
func PNG(data []byte) Content {
	return Content{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: "image/png"}
}

// Result is the result of tools/call.
type Result struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError"`
}

// ErrorResult is a failed tool call with an explanation.
func ErrorResult(format string, args ...any) *Result {
	return &Result{Content: []Content{Text(fmt.Sprintf(format, args...))}, IsError: true}
}

// Tool is a callable tool.
type Tool struct {
	Name        string
	Title       string
	Description string
	InputSchema json.RawMessage // JSON Schema object (additionalProperties: false)
	Handler     func(ctx context.Context, args json.RawMessage) (*Result, error)
}

// Server serves tools over JSON-RPC.
type Server struct {
	Name    string
	Version string
	Tools   []Tool
	Log     io.Writer // diagnostics; nil discards

	mu          sync.Mutex
	initialized bool
	protocol    string
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, "mcp: "+format+"\n", args...)
	}
}

// Serve reads requests from r and writes responses to w until r ends or ctx is done.
// Requests are handled one at a time, in order.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var out any
		if line[0] == '[' {
			var batch []json.RawMessage
			if err := json.Unmarshal(line, &batch); err != nil {
				out = response{JSONRPC: "2.0", ID: nil, Error: &rpcError{codeParse, "parse error: " + err.Error()}}
			} else {
				var resps []response
				for _, m := range batch {
					if resp := s.handle(ctx, m); resp != nil {
						resps = append(resps, *resp)
					}
				}
				if len(resps) > 0 {
					out = resps
				}
			}
		} else if resp := s.handle(ctx, line); resp != nil {
			out = resp
		}
		if out != nil {
			if err := enc.Encode(out); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// handle processes one message; notifications return nil.
func (s *Server) handle(ctx context.Context, raw []byte) *response {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return &response{JSONRPC: "2.0", Error: &rpcError{codeParse, "parse error: " + err.Error()}}
	}
	var id any
	notification := len(req.ID) == 0 || string(req.ID) == "null"
	if !notification {
		if err := json.Unmarshal(req.ID, &id); err != nil {
			return &response{JSONRPC: "2.0", Error: &rpcError{codeInvalidRequest, "invalid id"}}
		}
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		if notification {
			return nil
		}
		return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{codeInvalidRequest, "invalid request"}}
	}
	result, rerr := s.dispatch(ctx, req)
	if notification {
		return nil
	}
	resp := &response{JSONRPC: "2.0", ID: id}
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = result
	}
	return resp
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, &rpcError{codeInvalidParams, "initialize: " + err.Error()}
			}
		}
		version := SupportedVersions[0]
		for _, v := range SupportedVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		s.mu.Lock()
		s.protocol = version
		s.mu.Unlock()
		s.logf("initialize from %s %s, protocol %s", p.ClientInfo.Name, p.ClientInfo.Version, version)
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}, nil
	case "notifications/initialized":
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools := make([]map[string]any, 0, len(s.Tools))
		for _, t := range s.Tools {
			m := map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema}
			if t.Title != "" {
				m["title"] = t.Title
			}
			tools = append(tools, m)
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{codeInvalidParams, "tools/call: " + err.Error()}
		}
		for _, t := range s.Tools {
			if t.Name != p.Name {
				continue
			}
			args := p.Arguments
			if len(args) == 0 || string(args) == "null" {
				args = json.RawMessage("{}")
			}
			res, err := s.call(ctx, t, args)
			if err != nil {
				res = ErrorResult("%s: %v", t.Name, err)
			}
			if res.Content == nil {
				res.Content = []Content{}
			}
			return res, nil
		}
		return nil, &rpcError{codeInvalidParams, fmt.Sprintf("unknown tool %q", p.Name)}
	}
	if len(req.Method) > 14 && req.Method[:14] == "notifications/" {
		return nil, nil
	}
	return nil, &rpcError{codeMethodNotFound, "method not found: " + req.Method}
}

// call runs a handler, turning panics into error results so one bad call cannot kill
// the server.
func (s *Server) call(ctx context.Context, t Tool, args json.RawMessage) (res *Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("tool %s panicked: %v", t.Name, r)
			res, err = nil, fmt.Errorf("internal error: %v", r)
		}
	}()
	res, err = t.Handler(ctx, args)
	if err == nil && res == nil {
		err = errors.New("no result")
	}
	return res, err
}

// Strict decodes tool arguments into v, rejecting unknown fields (the schemas say
// additionalProperties: false).
func Strict(args json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}
