package main

import (
	"fmt"
	"os"

	"broker/cmd/broker/cli"
)

var version = "dev"

func main() {
	cli.SetVersion(version)
	if err := cli.Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
