package jsonutil

import (
	"bytes"
	"slices"

	"github.com/fatih/structs"
	"github.com/hokaccha/go-prettyjson"
)

// formatter renders one struct field per line, compactly but colorized for
// terminal diagnostics (session dumps in logs and the debug endpoints).
var formatter *prettyjson.Formatter

func init() {
	formatter = prettyjson.NewFormatter()
	formatter.Indent = 0
	formatter.Newline = ""
}

// MarshalCompactPretty renders struct v field by field, in sorted field
// order for stable, diffable output.
func MarshalCompactPretty(v any) ([]byte, error) {
	fields := structs.Map(v)
	names := structs.Names(v)
	slices.Sort(names)

	var buf bytes.Buffer
	for _, name := range names {
		b, err := formatter.Marshal(fields[name])
		if err != nil {
			return nil, err
		}
		buf.WriteString(name)
		buf.WriteString(": ")
		buf.Write(b)
		buf.WriteRune('\n')
	}
	return buf.Bytes(), nil
}
