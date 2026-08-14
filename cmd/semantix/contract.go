package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// writeEnvelope writes a success envelope (ok:true, error:null) to w,
// using the shared §4.2 envelope type from envelope.go.
func writeEnvelope(w io.Writer, command string, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(okEnvelope(command, data))
}

// writeErrorEnvelope writes a failure envelope (ok:false) with the U19 §4.3
// exit-code semantics in error.code: 1 runtime, 2 usage.
func writeErrorEnvelope(w io.Writer, command string, code int, message string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(errEnvelope(command, code, message))
}

// usageError is defined in main.go (U19 registry dispatch). usageErrf is
// kept here as the constructor used by export/import/gc.
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
