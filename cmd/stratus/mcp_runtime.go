package main

import (
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/mcp"
)

func parseMCPRuntime(args []string) (mcp.RuntimeOptions, error) {
	o := mcp.RuntimeOptions{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--runtime" && i+1 < len(args) {
			i++
			o.Runtime = args[i]
		} else if args[i] == "--runtime=codex" {
			o.Runtime = "codex"
		} else {
			return o, fmt.Errorf("unknown MCP option %q", args[i])
		}
	}
	if o.Runtime != "" && o.Runtime != "codex" {
		return o, fmt.Errorf("unsupported runtime %q", o.Runtime)
	}
	return o, nil
}
