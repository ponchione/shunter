package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ponchione/shunter/contractworkflow"
)

func formatTextOrJSON(format string, text func() string, value any) ([]byte, error) {
	switch normalizedOutputFormat(format) {
	case contractworkflow.FormatText:
		return []byte(text()), nil
	case contractworkflow.FormatJSON:
		return marshalIndentedJSON(value)
	default:
		return nil, fmt.Errorf("%w %q", contractworkflow.ErrUnsupportedFormat, format)
	}
}

func normalizedOutputFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return contractworkflow.FormatText
	}
	return format
}

func marshalIndentedJSON(value any) ([]byte, error) {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
