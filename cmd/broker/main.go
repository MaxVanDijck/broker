package main

import (
	"fmt"
	"os"

	"broker/cmd/broker/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
