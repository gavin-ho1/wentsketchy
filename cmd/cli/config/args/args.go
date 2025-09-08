package args

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/internal/fifo"
)

// https://felixkratz.github.io/SketchyBar/config/events
type In struct {
	// the item name
	Name string `json:"name"`
	// the event
	Event    string `json:"event"`
	Info     string `json:"info"`
	Button   string `json:"button"`
	Modifier string `json:"modifier"`
}

// $INFO is a json, and its not easy to embed a json inside a json
type Out struct {
	Name     string `json:"name"`
	Event    string `json:"event"`
	Button   string `json:"button"`
	Modifier string `json:"modifier"`
}

func FromEvent(msg string) (*In, error) {
	argsPrefix := "args: "
	infoPrefix := "info:" // Note: no surrounding spaces

	// Find the start of the JSON data
	argsStart := strings.Index(msg, argsPrefix)
	if argsStart == -1 {
		return nil, fmt.Errorf("args: could not find args prefix in message: %s", msg)
	}
	// The actual JSON and info data starts after the prefix
	jsonAndInfo := msg[argsStart+len(argsPrefix):]

	// Split the rest of the string by the info prefix
	parts := strings.SplitN(jsonAndInfo, infoPrefix, 2)
	argsJSON := parts[0]
	infoJSON := ""
	if len(parts) > 1 {
		infoJSON = parts[1]
	}

	// Trim any whitespace from the JSON part, which handles the space(s)
	// that were between the JSON and the info prefix.
	argsJSON = strings.TrimSpace(argsJSON)
	infoJSON = strings.TrimSpace(infoJSON)

	var args *In
	err := json.Unmarshal([]byte(argsJSON), &args)

	if err != nil {
		return nil, fmt.Errorf("args: could not deserialize data: %w. Got: %s", err, argsJSON)
	}

	if args == nil {
		// This can happen if the JSON is "null" or empty
		return nil, fmt.Errorf("args: deserialized data is nil. Got: %s", argsJSON)
	}

	args.Info = infoJSON

	return args, nil
}

func BuildEvent() (string, error) {
	data := &Out{
		Name:     "$NAME",
		Event:    "$SENDER",
		Button:   "$BUTTON",
		Modifier: "$MODIFIER",
	}

	bytes, err := json.Marshal(data)

	if err != nil {
		return "", fmt.Errorf("args: could not serialize data. %w", err)
	}

	serialized := strings.ReplaceAll(string(bytes), `"`, `\"`)

	// TODO: ensure file exists, also in aerospace.toml
	return fmt.Sprintf(
		// `[[ -f %s ]] && echo "update args: %s info: $INFO %c" >> %s`,
		`echo "update args: %s info: $INFO %c" >> %s`,
		// settings.FifoPath,
		serialized,
		fifo.Separator,
		settings.FifoPath,
	), nil
}
