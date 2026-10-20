package main

import (
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/integrations/codex"
	"os"
)

func cmdCodexHook() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: stratus codex-hook context|activity|guard")
		os.Exit(1)
	}
	os.Exit(codex.Run(os.Args[2], os.Stdin, os.Stdout, os.Stderr, nil))
}
