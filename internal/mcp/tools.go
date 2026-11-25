package mcp

import "context"

// Class is how much a tool may change.
type Class string

const (
	// ReadOnly tools only read the running FormNET.
	ReadOnly Class = "read_only"
	// Confined tools write files, and only under <data>/exports.
	Confined Class = "writes_confined"
	// Approval tools change the recording or the proxy; a client should ask the person first.
	Approval Class = "approval_gated"
)

// Tool is one MCP tool: its contract and the code behind it.
type Tool struct {
	Name        string
	Title       string
	Description string
	Class       Class
	Destructive bool
	Idempotent  bool
	Props       []prop
	Required    []string
	Output      map[string]any
	Run         func(ctx context.Context, s *Server, a args) (any, error)
}

func (t *Tool) describe() map[string]any {
	ann := map[string]any{"title": t.Title, "readOnlyHint": t.Class == ReadOnly, "openWorldHint": false}
	if t.Class != ReadOnly {
		ann["destructiveHint"] = t.Destructive
		ann["idempotentHint"] = t.Idempotent
	}
	return map[string]any{
		"name":         t.Name,
		"title":        t.Title,
		"description":  t.Description,
		"inputSchema":  inputSchema(t.Props, t.Required),
		"outputSchema": t.Output,
		"annotations":  ann,
	}
}

func registry() []*Tool {
	list := append(readTools(), writeTools()...)
	for _, t := range list {
		t.Props = compile(t.Props)
	}
	return list
}

// Definitions are the tools as tools/list returns them, in a fixed order.
func Definitions() []map[string]any {
	list := registry()
	out := make([]map[string]any, len(list))
	for i, t := range list {
		out[i] = t.describe()
	}
	return out
}

// Classes names every tool's class, in the order tools/list returns them.
func Classes() []struct {
	Name  string
	Class Class
} {
	list := registry()
	out := make([]struct {
		Name  string
		Class Class
	}, len(list))
	for i, t := range list {
		out[i].Name, out[i].Class = t.Name, t.Class
	}
	return out
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func arr(items map[string]any) map[string]any { return map[string]any{"type": "array", "items": items} }

func typ(t, description string) map[string]any {
	return map[string]any{"type": t, "description": description}
}

func nullable(t, description string) map[string]any {
	return map[string]any{"type": []string{t, "null"}, "description": description}
}
