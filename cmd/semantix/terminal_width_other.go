//go:build !darwin && !linux

package main

import "io"

func terminalSize(io.Writer) (columns, rows int) { return 0, 0 }
