package tool

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The semantix_lookup tool shells out to a `semantix` binary found on PATH, so
// testing it means supplying a fake kernel. A shell script is the obvious
// fixture and the wrong one: it drags in whichever shell the host happens to
// have, and on Windows `cmd.exe` writes stdout in the console OEM code page,
// so a UTF-8 argv comes back as mojibake and the assertion needs a second
// platform-specific `want`. Instead the test binary re-executes *itself* as
// the fake kernel — argv crosses the process boundary as UTF-8 in both
// directions, no shell is involved, and one assertion holds on every platform.
const fakeKernelEnv = "SEMANTIX_TEST_FAKE_KERNEL"

// Fake kernel behaviours, selected through fakeKernelEnv.
const (
	fakeKernelArgv = "argv" // echo argv, exit 0
	fakeKernelFail = "fail" // exit non-zero without writing stdout
	fakeKernelHang = "hang" // block until the caller's deadline kills us
)

// TestMain hands the process over to the fake kernel when fakeKernelEnv is
// set. Only fakeKernelPATH sets it, and t.Setenv scopes it to one test; an
// unrecognised value means the variable leaked in from the developer's shell,
// which would otherwise turn every test in this package into an argv echo, so
// it fails loudly instead.
func TestMain(m *testing.M) {
	mode, ok := os.LookupEnv(fakeKernelEnv)
	if !ok {
		os.Exit(m.Run())
	}
	switch mode {
	case fakeKernelArgv:
		fmt.Println(strings.Join(os.Args[1:], " "))
		os.Exit(0)
	case fakeKernelFail:
		os.Exit(1)
	case fakeKernelHang:
		// exec.CommandContext kills us at the caller's deadline; the sleep is
		// only a backstop so a missed kill cannot strand the process.
		time.Sleep(2 * time.Minute)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "%s=%q is not a fake kernel mode\n", fakeKernelEnv, mode)
		os.Exit(2)
	}
}

// fakeKernelPATH points PATH at a directory holding nothing but a `semantix`
// that behaves as mode says. PATH is replaced rather than prepended: the
// machine running the tests may well have a real semantix installed — this is
// its repository — and a test that reaches it is measuring the developer's
// slice store instead of the tool.
func fakeKernelPATH(t *testing.T, mode string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	name := "semantix"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := t.TempDir()
	// Copy rather than hardlink: on Windows a link to the running test binary
	// shares its locked image, and t.TempDir's cleanup then fails with
	// "Access is denied".
	copyFile(t, self, filepath.Join(dir, name))
	t.Setenv(fakeKernelEnv, mode)
	t.Setenv("PATH", dir)
}

// noKernelPATH makes the lookup binary unreachable, so the tool takes its
// "kernel unavailable" path by construction rather than by assuming the host
// has no semantix installed.
func noKernelPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatalf("copy to %s: %v", dst, err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close %s: %v", dst, err)
	}
}
