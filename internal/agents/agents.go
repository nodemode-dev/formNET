// Package agents writes what FormNET offers agents into the repository: the
// MCP tool list (agents/manifest.json, agents/tools.json), the tool table in
// the skill pack, and the mirror of .claude/skills under .agents/skills. It
// also holds the skills to the format contract formcompiler and the agent
// surfaces expect. Sync writes; Check reports every file that is stale.
package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nodemode-dev/formnet/internal/mcp"
)

const (
	// SkillsRoot is the skills folder formcompiler's registry names.
	SkillsRoot = ".claude/skills"
	// Mirror is the byte-identical copy for agents that read .agents/skills.
	Mirror       = ".agents/skills"
	manifestPath = "agents/manifest.json"
	toolsPath    = "agents/tools.json"
	toolTable    = "references/mcp-tools.md"
	module       = "module github.com/nodemode-dev/formnet"
)

// Root finds the repository root at or above dir by its go.mod.
func Root(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && bytes.HasPrefix(raw, []byte(module+"\n")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside the formNET repository (no go.mod for " + strings.TrimPrefix(module, "module ") + " found above this folder)")
		}
		dir = parent
	}
}

type skill struct {
	Folder      string   `json:"folder"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	SubSkills   []string `json:"sub_skills"`
	Files       int      `json:"files"`
}

type mcpManifest struct {
	Protocol      string   `json:"protocol"`
	Legacy        []string `json:"legacy_protocols"`
	Command       []string `json:"command"`
	Transports    []string `json:"transports"`
	Count         int      `json:"count"`
	Tools         []string `json:"tools"`
	ReadOnly      []string `json:"read_only"`
	Confined      []string `json:"writes_confined"`
	ApprovalGated []string `json:"approval_gated"`
}

type manifest struct {
	Format     string      `json:"format"`
	Version    int         `json:"version"`
	App        string      `json:"app"`
	MCP        mcpManifest `json:"mcp"`
	SkillsRoot string      `json:"skills_root"`
	Mirror     string      `json:"skills_mirror"`
	Skills     []skill     `json:"skills"`
}

func encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// expected returns every generated file, by slash path from the root.
func expected(root string) (map[string][]byte, []string, error) {
	problems := lint(root)
	m := manifest{Format: "form-agent-manifest", Version: 1, App: "formNET", SkillsRoot: SkillsRoot, Mirror: Mirror, Skills: []skill{}}
	m.MCP = mcpManifest{Protocol: mcp.Modern, Legacy: mcp.Legacy, Command: []string{"formnet", "mcp"}, Transports: []string{"stdio"},
		Tools: []string{}, ReadOnly: []string{}, Confined: []string{}, ApprovalGated: []string{}}
	for _, c := range mcp.Classes() {
		m.MCP.Tools = append(m.MCP.Tools, c.Name)
		switch c.Class {
		case mcp.ReadOnly:
			m.MCP.ReadOnly = append(m.MCP.ReadOnly, c.Name)
		case mcp.Confined:
			m.MCP.Confined = append(m.MCP.Confined, c.Name)
		case mcp.Approval:
			m.MCP.ApprovalGated = append(m.MCP.ApprovalGated, c.Name)
		}
	}
	m.MCP.Count = len(m.MCP.Tools)
	out := map[string][]byte{}
	tools, err := encode(map[string]any{"protocol": mcp.Modern, "tools": mcp.Definitions()})
	if err != nil {
		return nil, nil, err
	}
	out[toolsPath] = tools

	src := filepath.Join(root, filepath.FromSlash(SkillsRoot))
	folders, err := os.ReadDir(src)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", SkillsRoot, err)
	}
	for _, d := range folders {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		card, err := os.ReadFile(filepath.Join(src, d.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		front := frontMatter(string(card))
		sk := skill{Folder: d.Name(), Name: front["name"], Description: front["description"], SubSkills: []string{}}
		subs, _ := os.ReadDir(filepath.Join(src, d.Name(), "sub-skills"))
		for _, s := range subs {
			if s.IsDir() {
				sk.SubSkills = append(sk.SubSkills, s.Name())
			}
		}
		table := path.Join(SkillsRoot, d.Name(), toolTable)
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(table))); err == nil {
			out[table] = toolMarkdown(m.MCP)
		}
		m.Skills = append(m.Skills, sk)
	}
	if len(m.Skills) == 0 {
		problems = append(problems, SkillsRoot+" holds no folder with a SKILL.md")
	}

	files, err := tree(root, SkillsRoot)
	if err != nil {
		return nil, nil, err
	}
	for rel, body := range files {
		if gen, ok := out[path.Join(SkillsRoot, rel)]; ok {
			body = gen
		}
		out[path.Join(Mirror, rel)] = body
	}
	for i := range m.Skills {
		for rel := range files {
			if strings.HasPrefix(rel, m.Skills[i].Folder+"/") {
				m.Skills[i].Files++
			}
		}
	}
	raw, err := encode(m)
	if err != nil {
		return nil, nil, err
	}
	out[manifestPath] = raw
	return out, problems, nil
}

func toolMarkdown(m mcpManifest) []byte {
	var b strings.Builder
	b.WriteString("# FormNET MCP tools\n\nWritten by `formnet agents sync` from the server's own tool list; `formnet agents check` fails when it is stale. Do not edit by hand.\n\n")
	fmt.Fprintf(&b, "Protocol %s on stdio (`formnet mcp`), and the initialize handshake of %s for older clients. %d tools: %d read-only, %d writing only under `[data]/exports`, %d needing the person's approval.\n\n",
		m.Protocol, strings.Join(m.Legacy, ", "), m.Count, len(m.ReadOnly), len(m.Confined), len(m.ApprovalGated))
	b.WriteString("| Tool | Class | What it does |\n| --- | --- | --- |\n")
	class := map[string]string{}
	for _, n := range m.ReadOnly {
		class[n] = "read-only"
	}
	for _, n := range m.Confined {
		class[n] = "writes under exports"
	}
	for _, n := range m.ApprovalGated {
		class[n] = "needs approval"
	}
	for _, d := range mcp.Definitions() {
		name := d["name"].(string)
		desc := strings.ReplaceAll(d["description"].(string), "|", "/")
		desc = strings.ReplaceAll(desc, "<data>", "[data]")
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", name, class[name], desc)
	}
	b.WriteString("\nFull input and output schemas are in `agents/tools.json` at the repository root.\n")
	return []byte(b.String())
}

var skipDirs = []string{"__pycache__", "node_modules", ".git"}
var skipFiles = []string{".DS_Store", "Thumbs.db"}

// tree reads every file under base the way formcompiler packs it, by slash
// path relative to base. A symbolic link anywhere is refused.
func tree(root, base string) (map[string][]byte, error) {
	dir := filepath.Join(root, filepath.FromSlash(base))
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link; skills hold plain files only", p)
		}
		if d.IsDir() {
			if p != dir && slices.Contains(skipDirs, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if slices.Contains(skipFiles, d.Name()) || strings.HasSuffix(d.Name(), ".pyc") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = raw
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	return out, err
}

// Sync writes every generated file and removes mirror files with no source.
func Sync(root string) ([]string, error) {
	want, problems, err := expected(root)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New("the skills break the format contract:\n  " + strings.Join(problems, "\n  "))
	}
	var wrote []string
	for _, rel := range sortedKeys(want) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if have, err := os.ReadFile(p); err == nil && bytes.Equal(have, want[rel]) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return wrote, err
		}
		if err := os.WriteFile(p, want[rel], 0o644); err != nil {
			return wrote, err
		}
		wrote = append(wrote, rel)
	}
	have, err := tree(root, Mirror)
	if err != nil {
		return wrote, err
	}
	for _, rel := range sortedKeys(have) {
		if _, ok := want[path.Join(Mirror, rel)]; !ok {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(Mirror), filepath.FromSlash(rel))); err != nil {
				return wrote, err
			}
			wrote = append(wrote, "removed "+path.Join(Mirror, rel))
		}
	}
	return wrote, nil
}

// Check returns every problem: a generated file that is missing or stale, a
// mirror file with no source, and every break of the format contract.
func Check(root string) ([]string, error) {
	want, problems, err := expected(root)
	if err != nil {
		return nil, err
	}
	for _, rel := range sortedKeys(want) {
		have, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			problems = append(problems, rel+" is missing")
		case !bytes.Equal(have, want[rel]):
			problems = append(problems, rel+" is stale")
		}
	}
	have, err := tree(root, Mirror)
	if err != nil {
		return nil, err
	}
	for _, rel := range sortedKeys(have) {
		if _, ok := want[path.Join(Mirror, rel)]; !ok {
			problems = append(problems, path.Join(Mirror, rel)+" has no source in "+SkillsRoot)
		}
	}
	return problems, nil
}

// Count is the number of MCP tools, for documents that state it.
func Count() int { return len(mcp.Classes()) }

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}

// frontMatter reads the flat key: value lines between the opening --- lines.
func frontMatter(text string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok || strings.HasPrefix(l, " ") {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

var (
	nameRule = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	tagRule  = regexp.MustCompile(`<[A-Za-z/!?][^>]*>`)
)

func pictographic(r rune) bool {
	return (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) || (r >= 0x2B00 && r <= 0x2BFF) || r == 0xFE0F || r == 0x200D
}

// lint holds every skill under SkillsRoot to the format contract.
func lint(root string) []string {
	var problems []string
	base := filepath.Join(root, filepath.FromSlash(SkillsRoot))
	_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			problems = append(problems, err.Error())
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		lower := strings.ToLower(d.Name())
		if !d.IsDir() && strings.HasPrefix(lower, "readme") {
			problems = append(problems, rel+": no README inside a skill; put the text in SKILL.md or a reference")
		}
		if !d.IsDir() && (lower == "skill.md" || lower == "sub-skill.md") && d.Name() != "SKILL.md" && d.Name() != "SUB-SKILL.md" {
			problems = append(problems, rel+": the file must be named exactly SKILL.md or SUB-SKILL.md")
		}
		if d.IsDir() || !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".json") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			problems = append(problems, err.Error())
			return nil
		}
		text := string(raw)
		if !utf8.ValidString(text) {
			problems = append(problems, rel+": not UTF-8")
		}
		if strings.ContainsRune(text, 0x2014) {
			problems = append(problems, rel+": em dash; use an interpunct or restructure")
		}
		for _, r := range text {
			if pictographic(r) {
				problems = append(problems, fmt.Sprintf("%s: emoji or pictograph U+%04X", rel, r))
				break
			}
		}
		if strings.HasSuffix(lower, ".md") && tagRule.MatchString(text) {
			problems = append(problems, rel+": tag-shaped text "+tagRule.FindString(text)+"; use [square-bracket] placeholders")
		}
		if d.Name() == "SKILL.md" || d.Name() == "SUB-SKILL.md" {
			problems = append(problems, card(rel, filepath.Base(filepath.Dir(p)), text)...)
		}
		return nil
	})
	return problems
}

func card(rel, folder, text string) []string {
	var problems []string
	f := frontMatter(text)
	name, desc := f["name"], f["description"]
	switch {
	case name == "":
		problems = append(problems, rel+": no name in the front matter")
	case len(name) > 64 || !nameRule.MatchString(name):
		problems = append(problems, rel+": name must be 1 to 64 lowercase letters, digits and single hyphens")
	case name != folder:
		problems = append(problems, rel+": name "+name+" does not match its folder "+folder)
	case strings.Contains(name, "anthropic") || strings.Contains(name, "claude"):
		problems = append(problems, rel+": name must not contain a reserved word")
	}
	if n := utf8.RuneCountInString(desc); n == 0 || n > 1024 {
		problems = append(problems, fmt.Sprintf("%s: description must be 1 to 1024 characters, it is %d", rel, n))
	}
	if !strings.Contains(strings.ToLower(desc), "use ") {
		problems = append(problems, rel+": description must say when to use it")
	}
	return problems
}
