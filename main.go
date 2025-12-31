package main

import (
	"os"

	"github.com/matias/regrada/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
