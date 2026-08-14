package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// cliVersion is reported in every JSON envelope (U19 §4.2) and matches the
// release line in docs/reports/cli-v2-architecture.md.
const cliVersion = "0.3.1"

// envelope is the uniform --json output shape (U19 §4.2):
//
//	{"ok": true, "command": "gc", "data": {...}, "error": null, "version": "0.3.1"}
//
// On failure ok is false and error carries {code, message}; the process exit
// code still follows §4.3.
type envelope struct {
	OK      bool        `json:"ok"`
	Command string      `json:"command"`
	Data    interface{} `json:"data"`
	Error   *envError   `json:"error"`
	Version string      `json:"version"`
}

type envError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// writeEnvelope writes a success envelope (ok:true, error:null) to w.
func writeEnvelope(w io.Writer, command string, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope{OK: true, Command: command, Data: data, Version: cliVersion})
}

// writeErrorEnvelope writes a failure envelope (ok:false) with the U19 §4.3
// exit-code semantics in error.code: 1 runtime, 2 usage.
func writeErrorEnvelope(w io.Writer, command string, code int, message string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope{
		OK:      false,
		Command: command,
		Error:   &envError{Code: code, Message: message},
		Version: cliVersion,
	})
}

// usageError marks a command-line usage problem (bad flag, missing required
// argument). run() maps it to exit code 2 per U19 §4.3; plain errors map to 1.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// usageErrf builds a usageError from a format string.
func usageErrf(format string, args ...interface{}) error {
	return usageError{err: fmt.Errorf(format, args...)}
}

// failJSON writes the failure envelope (ok:false, error.code per U19 §4.3)
// for a runtime error and returns the original error so run() still exits 1.
func failJSON(w io.Writer, command string, err error) error {
	_ = writeErrorEnvelope(w, command, 1, err.Error())
	return err
}

// wantsJSON reports whether raw args request --json. Used when flag parsing
// failed (so the parsed flag set is unavailable) to decide whether a failure
// envelope must be emitted for a --json consumer. Handles both dash forms the
// flag package accepts (-json / --json) and skips explicit disable forms.
func wantsJSON(args []string) bool {
	for _, a := range args {
		switch {
		case a == "-json" || a == "--json":
			return true
		case strings.HasPrefix(a, "--json=") || strings.HasPrefix(a, "-json="):
			v := strings.TrimPrefix(strings.TrimPrefix(a, "--json="), "-json=")
			if v != "false" && v != "0" {
				return true
			}
		}
	}
	return false
}
