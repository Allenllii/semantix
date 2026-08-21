package main

import (
	"errors"
	"flag"
	"io"

	"semantix/internal/intro"
)

// runIntro adapts `semantix intro` to the shared renderer in internal/intro
// (Issue #307). Flag parsing stays here so the U19 exit-code contract
// (help=0, usage=2) is enforced the same way as every other command.
func runIntro(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("intro", flag.ContinueOnError)
	fs.SetOutput(stdout)
	noAnimation := fs.Bool("no-animation", false, "render the final frame without animation")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request (U19 contract)
		}
		return 2
	}
	return intro.Run(stdout, intro.Options{Version: version, NoAnimation: *noAnimation})
}
