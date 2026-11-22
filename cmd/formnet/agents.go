package main

import (
	"fmt"
	"io"

	"github.com/nodemode-dev/formnet/internal/agents"
)

// agentsCommand writes or checks the agent files the repository carries.
func agentsCommand(args []string, stdout, stderr io.Writer) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	if sub != "sync" && sub != "check" {
		fmt.Fprintln(stderr, "usage: formnet agents sync | check   (run inside the formNET repository)")
		return 2
	}
	root, err := agents.Root(".")
	if err != nil {
		fmt.Fprintln(stderr, "formnet agents:", err)
		return 2
	}
	if sub == "sync" {
		wrote, err := agents.Sync(root)
		for _, w := range wrote {
			fmt.Fprintln(stdout, "wrote", w)
		}
		if err != nil {
			fmt.Fprintln(stderr, "formnet agents sync:", err)
			return 1
		}
		fmt.Fprintf(stdout, "agents: %d MCP tools; %d files changed\n", agents.Count(), len(wrote))
		return 0
	}
	problems, err := agents.Check(root)
	if err != nil {
		fmt.Fprintln(stderr, "formnet agents check:", err)
		return 1
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(stderr, "formnet agents check:", p)
		}
		fmt.Fprintln(stderr, "run formnet agents sync, fix what it cannot, and commit the result")
		return 1
	}
	fmt.Fprintf(stdout, "agents: up to date, %d MCP tools\n", agents.Count())
	return 0
}
