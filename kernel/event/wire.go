package event

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ToJSON serializes an Event to its wire representation.
func ToJSON(e Event) ([]byte, error) {
	return json.Marshal(e)
}

// FromJSON parses an Event from its wire representation and validates the Kind.
func FromJSON(b []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return Event{}, err
	}
	if e.Kind < 0 || e.Kind >= KindCount {
		return Event{}, fmt.Errorf("event: invalid kind %d", e.Kind)
	}
	return e, nil
}

// UnmarshalData decodes e.Data into dst (the payload type for e.Kind).
// An empty Data decodes to a zero payload without error (empty-payload kinds
// such as TurnStarted). Unknown fields are rejected so wire contract drift
// (renamed/added payload fields) surfaces as an error instead of silent loss.
func UnmarshalData(e Event, dst any) error {
	if len(e.Data) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(e.Data))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
