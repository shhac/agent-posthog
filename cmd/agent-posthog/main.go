package main

import (
	"github.com/shhac/agent-posthog/internal/cli"
)

var version = "dev"

func main() {
	cli.Run(version)
}
