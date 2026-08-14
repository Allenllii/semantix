package main

// envelope is the unified JSON envelope (§4.2): every command that supports
// --json wraps its payload in {ok, command, data, error, version}.
type envelope struct {
	OK      bool    `json:"ok"`
	Command string  `json:"command"`
	Data    any     `json:"data"`
	Error   *envErr `json:"error"`
	Version string  `json:"version"`
}

// envErr mirrors the exit-code semantics (1 runtime / 2 usage) in the JSON
// error payload.
type envErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func okEnvelope(command string, data any) envelope {
	return envelope{OK: true, Command: command, Data: data, Version: version}
}

func errEnvelope(command string, code int, msg string) envelope {
	return envelope{OK: false, Command: command, Error: &envErr{Code: code, Message: msg}, Version: version}
}
