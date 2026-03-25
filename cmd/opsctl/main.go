package main

import (
	"os"

	"github.com/opskat/opskat/cmd/opsctl/command"
)

func main() {
	os.Exit(command.Execute())
}
