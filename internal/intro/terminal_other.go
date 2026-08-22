//go:build !darwin && !linux

package intro

import "io"

func terminalSize(io.Writer) (columns, rows int) { return 0, 0 }
