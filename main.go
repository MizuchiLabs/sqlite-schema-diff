package main

import (
	"fmt"
	"os"

	"github.com/mizuchilabs/kata/buildinfo"
	"github.com/mizuchilabs/kata/sigx"

	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		EnableShellCompletion: true,
		Suggest:               true,
		Name:                  "sqlite-schema-diff",
		Version:               buildinfo.String(),
		Usage:                 "simple migrations for SQLite",
		DefaultCommand:        "help",
		Commands:              commands,
	}

	if err := cmd.Run(sigx.NotifyContext(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmd.Name, err)
		os.Exit(1)
	}
}
