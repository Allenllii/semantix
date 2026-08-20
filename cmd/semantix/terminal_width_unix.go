//go:build darwin || linux

package main

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

type terminalWindowSize struct {
	rows    uint16
	columns uint16
	xpixel  uint16
	ypixel  uint16
}

func terminalSize(w io.Writer) (columns, rows int) {
	file, ok := w.(*os.File)
	if !ok {
		return 0, 0
	}
	var size terminalWindowSize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0, 0
	}
	return int(size.columns), int(size.rows)
}
