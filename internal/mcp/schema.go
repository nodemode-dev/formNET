package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// prop is one tool argument: it is both the JSON Schema a client reads and
// the rule a call is checked against, so the two never drift apart.
type prop struct {
	Name        string
	Type        string // string, integer, boolean, array (of strings)
	Description string
	Enum        []string
	Min, Max    *int64
	Default     any
	Pattern     string
	MaxLength   int
	MaxItems    int
	re          *regexp.Regexp
}

func intRange(lo, hi int64) (*int64, *int64) { return &lo, &hi }

func (p prop) schema() map[string]any {
	s := map[string]any{"type": p.Type, "description": p.Description}
	if len(p.Enum) > 0 {
		s["enum"] = p.Enum
	}
	if p.Min != nil {
		s["minimum"] = *p.Min
	}
	if p.Max != nil {
		s["maximum"] = *p.Max
	}
	if p.Default != nil {
		s["default"] = p.Default
	}
	item := map[string]any{"type": "string"}
	target := s
	if p.Type == "array" {
		s["items"] = item
		target = item
		if p.MaxItems > 0 {
			s["maxItems"] = p.MaxItems
		}
	}
	if p.Pattern != "" {
		target["pattern"] = p.Pattern
	}
	if p.MaxLength > 0 {
		target["maxLength"] = p.MaxLength
	}
	return s
}

func inputSchema(props []prop, required []string) map[string]any {
	ps := map[string]any{}
	for _, p := range props {
		ps[p.Name] = p.schema()
	}
	s := map[string]any{"type": "object", "properties": ps, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// args are the checked arguments of one call.
type args map[string]any

func (a args) str(name string) string {
	v, _ := a[name].(string)
	return v
}

func (a args) num(name string) (int64, bool) {
	v, ok := a[name].(int64)
	return v, ok
}

func (a args) flag(name string) (bool, bool) {
	v, ok := a[name].(bool)
	return v, ok
}

func (a args) list(name string) ([]string, bool) {
	v, ok := a[name].([]string)
	return v, ok
}

// check reads raw arguments against props: unknown names, wrong types,
// values outside an enum, a range or a pattern are refused with a message
// the model can act on. Numbers and booleans sent as strings are accepted.
func check(props []prop, required []string, raw json.RawMessage) (args, error) {
	in := map[string]json.RawMessage{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("arguments must be a JSON object")
		}
	}
	known := map[string]prop{}
	for _, p := range props {
		known[p.Name] = p
	}
	out := args{}
	names := make([]string, 0, len(in))
	for k := range in {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		p, ok := known[k]
		if !ok {
			return nil, fmt.Errorf("unknown argument %q; this tool takes %s", k, propNames(props))
		}
		if string(in[k]) == "null" {
			continue
		}
		v, err := p.read(in[k])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out[k] = v
	}
	for _, r := range required {
		if _, ok := out[r]; !ok {
			return nil, fmt.Errorf("%s is required", r)
		}
	}
	return out, nil
}

func propNames(props []prop) string {
	if len(props) == 0 {
		return "no arguments"
	}
	n := make([]string, len(props))
	for i, p := range props {
		n[i] = p.Name
	}
	return strings.Join(n, ", ")
}

func (p prop) read(raw json.RawMessage) (any, error) {
	switch p.Type {
	case "string":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("must be a string")
		}
		return s, p.text(s)
	case "integer":
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil {
			var s string
			if json.Unmarshal(raw, &s) != nil {
				return nil, fmt.Errorf("must be a whole number")
			}
			v, perr := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if perr != nil {
				return nil, fmt.Errorf("must be a whole number")
			}
			n = float64(v)
		}
		if n != math.Trunc(n) || math.Abs(n) > 1<<53 {
			return nil, fmt.Errorf("must be a whole number")
		}
		v := int64(n)
		if p.Min != nil && v < *p.Min {
			return nil, fmt.Errorf("must be at least %d", *p.Min)
		}
		if p.Max != nil && v > *p.Max {
			return nil, fmt.Errorf("must be at most %d", *p.Max)
		}
		return v, nil
	case "boolean":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			var s string
			if json.Unmarshal(raw, &s) != nil {
				return nil, fmt.Errorf("must be true or false")
			}
			v, perr := strconv.ParseBool(strings.TrimSpace(s))
			if perr != nil {
				return nil, fmt.Errorf("must be true or false")
			}
			b = v
		}
		return b, nil
	case "array":
		var l []string
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("must be a list of strings")
		}
		if p.MaxItems > 0 && len(l) > p.MaxItems {
			return nil, fmt.Errorf("may hold at most %d entries", p.MaxItems)
		}
		for i, s := range l {
			if err := p.text(s); err != nil {
				return nil, fmt.Errorf("entry %d: %w", i, err)
			}
		}
		if l == nil {
			l = []string{}
		}
		return l, nil
	}
	return nil, fmt.Errorf("has an unknown type")
}

func (p prop) text(s string) error {
	if p.MaxLength > 0 && len([]rune(s)) > p.MaxLength {
		return fmt.Errorf("may be at most %d characters", p.MaxLength)
	}
	if len(p.Enum) > 0 && !slices.Contains(p.Enum, s) {
		return fmt.Errorf("must be one of %s", strings.Join(p.Enum, ", "))
	}
	if p.re != nil && !p.re.MatchString(s) {
		return fmt.Errorf("does not match %s", p.Pattern)
	}
	return nil
}

func compile(props []prop) []prop {
	out := make([]prop, len(props))
	for i, p := range props {
		if p.Pattern != "" {
			p.re = regexp.MustCompile(p.Pattern)
		}
		out[i] = p
	}
	return out
}
