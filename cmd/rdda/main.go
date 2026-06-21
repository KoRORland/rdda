package main

import (
	"fmt"
	"os"

	"github.com/KoRORland/rdda/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "rdda:", err)
		os.Exit(1)
	}
}
