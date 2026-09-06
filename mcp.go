package kgraph

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MCP over stdio, JSON-RPC 2.0. Deliberately no dependency: the surface is
// initialize / tools/list / tools/call.
//
// This is a THIN CLIENT of the daemon, not a second reader of the filesystem.
// One process owns scope resolution, branch keying and the reload cache; the
// shim only knows its own working directory and forwards it as `dir`. Two
// independent readers would eventually disagree about what is fresh, and the
// whole product is the freshness answer.
//
// The tool set exposes render and attach and NOTHING that writes a file: an
// agent driving kgraph through MCP cannot write an output without going through
// attach, so pins cannot silently fall behind. An agent with its own file tools
// can still bypass it — that degrades to `output-edited`, which is detected.

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type server struct {
	addr string // daemon address
	dir  string // this shim's working directory; routes to a project + branch
	now  string
	http *http.Client
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func (s *server) tools() []mcpTool {
	spec := str("Spec name, basename, or repo-relative path. Omit for all.")
	return []mcpTool{
		{"kg_status", "Which generated documents are stale, and why. Call this first.",
			obj(map[string]any{"spec": spec})},
		{"kg_diff", "What moved since each document was generated, as explicit statements " +
			"(\"count went 3 → 4\"). Revise the wrong sentence rather than rewriting the page.",
			obj(map[string]any{"spec": spec})},
		{"kg_variants", "Open questions a document rests on, and what each declared answer " +
			"would change. `diverges: true` means the answer changes the text — a reason to " +
			"wait. Otherwise the question only blocks, and either answer reads the same.",
			obj(map[string]any{"spec": spec})},
		{"kg_what_if", "Blast radius of a fact BEFORE committing it: which queries would " +
			"change and which documents would go stale. Writes nothing.",
			obj(map[string]any{"facts": str("a kfacts YAML fragment")}, "facts")},
		{"kg_query", "Resolve a query. Surface DSL, e.g. `question[status=open] @now sort id`. " +
			"Identifiers may be temporal aliases — date the query if one is ambiguous.",
			obj(map[string]any{"query": str("kgraph query DSL")}, "query")},
		{"kg_backlog", "Documents in the project and what has been read from each. `unread` " +
			"means no source node cites it, so it is invisible to every other command — that " +
			"is where unread evidence hides.", obj(map[string]any{})},
		{"kg_extract", "The extraction prompt for one document: its body, what is already " +
			"read from it, and the open evidence questions it might close. Write the facts " +
			"into a *.kfacts.md yourself, then call kg_scan. kgraph does not write facts.",
			obj(map[string]any{
				"doc": str("project-relative document path, from kg_backlog"),
			}, "doc")},
		{"kg_scan", "Parse everything and report problems: dangling references, reversed hops, " +
			"unplanned branches, ambiguous aliases, tampered managed blocks.", obj(map[string]any{})},
		{"kg_scopes", "Projects and branches the daemon currently holds.", obj(map[string]any{})},
	}
}

// ServeMCP runs the stdio shim, forwarding to the daemon at addr. It starts a
// daemon if none is listening, so an agent never has to be told to boot one.
func ServeMCP(addr, dir, now string, in io.Reader, out io.Writer) error {
	if addr == "" {
		addr = DefaultAddr
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	s := &server{addr: addr, dir: dir, now: now, http: &http.Client{Timeout: 60 * time.Second}}
	if err := s.ensureDaemon(); err != nil {
		return err
	}
	dec := json.NewDecoder(bufio.NewReader(in))
	enc := json.NewEncoder(out)
	for {
		var req rpcReq
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		resp := s.handle(req)
		if resp == nil {
			continue // a notification
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

// ensureDaemon starts one if the address is dead. The daemon is a singleton, so
// a race just means one of the two loses the port and exits.
func (s *server) ensureDaemon() error {
	if s.ping() == nil {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("no daemon at %s and cannot locate the kg binary: %w", s.addr, err)
	}
	cmd := exec.Command(self, "daemon", s.addr)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("no daemon at %s and could not start one: %w", s.addr, err)
	}
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s.ping() == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("started a daemon but %s never came up", s.addr)
}

func (s *server) ping() error {
	c := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(s.base() + "/scopes")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (s *server) base() string { return "http://" + s.addr }

func (s *server) handle(req rpcReq) *rpcResp {
	ok := func(v any) *rpcResp { return &rpcResp{JSONRPC: "2.0", ID: req.ID, Result: v} }
	fail := func(code int, f string, a ...any) *rpcResp {
		return &rpcResp{JSONRPC: "2.0", ID: req.ID, Error: &rpcErr{code, fmt.Sprintf(f, a...)}}
	}
	switch req.Method {
	case "initialize":
		return ok(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "kgraph", "version": "0.1.0"},
		})
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "tools/list":
		return ok(map[string]any{"tools": s.tools()})
	case "tools/call":
		var p struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(-32602, "bad params: %v", err)
		}
		body, err := s.call(p.Name, p.Args)
		if err != nil {
			// A tool error is content, not transport: the agent should read it
			// and correct, not have the call fail opaquely.
			return ok(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "error: " + err.Error()}},
				"isError": true,
			})
		}
		return ok(map[string]any{
			"content": []any{map[string]any{"type": "text", "text": body}},
		})
	}
	return fail(-32601, "unknown method %q", req.Method)
}

func (s *server) call(name string, raw json.RawMessage) (string, error) {
	var a struct {
		Spec  string   `json:"spec"`
		Query string   `json:"query"`
		Facts string   `json:"facts"`
		Token string   `json:"token"`
		Paths []string `json:"paths"`
		Doc   string   `json:"doc"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
	}
	q := url.Values{}
	q.Set("dir", s.dir)
	if s.now != "" {
		q.Set("now", s.now)
	}

	switch name {
	case "kg_scopes":
		return s.get("/scopes", q)
	case "kg_scan":
		return s.get("/scan", q)
	case "kg_status", "kg_diff", "kg_variants":
		if a.Spec != "" {
			q.Set("spec", a.Spec)
		}
		return s.get("/"+strings.TrimPrefix(name, "kg_"), q)
	case "kg_query":
		q.Set("q", a.Query)
		return s.get("/query", q)
	case "kg_backlog":
		return s.get("/backlog", q)
	case "kg_extract":
		if a.Doc == "" {
			return "", fmt.Errorf("extract needs a doc path; call kg_backlog for the list")
		}
		q.Set("doc", a.Doc)
		return s.get("/extract", q)
	case "kg_what_if":
		return s.post("/what-if", q, map[string]any{"facts": a.Facts})
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

func (s *server) get(path string, q url.Values) (string, error) {
	resp, err := s.http.Get(s.base() + path + "?" + q.Encode())
	if err != nil {
		return "", err
	}
	return readResult(resp)
}

func (s *server) post(path string, q url.Values, body any) (string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	resp, err := s.http.Post(s.base()+path+"?"+q.Encode(), "application/json", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	return readResult(resp)
}

// readResult turns a huma problem document into a plain sentence, so the agent
// sees "P74736 is ambiguous at any time" rather than an HTTP status.
func readResult(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		var problem struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(b, &problem) == nil && problem.Detail != "" {
			return "", fmt.Errorf("%s", problem.Detail)
		}
		return "", fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, b, "", "  ") == nil {
		return pretty.String(), nil
	}
	return string(b), nil
}
