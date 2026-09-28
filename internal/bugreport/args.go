package bugreport

import (
	"fmt"
	"strings"
)

// Args is the parsed /bug command line.
type Args struct {
	Hint       string
	Transcript bool
	Summary    bool
}

// ParseArgs reads `/bug` arguments. --transcript and --summary cannot both be set.
func ParseArgs(rest string) (Args, error) {
	fields := strings.Fields(rest)
	var hint []string
	var transcript, summary bool
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "--" {
			hint = append(hint, fields[i+1:]...)
			break
		}
		switch f {
		case "--transcript":
			transcript = true
		case "--summary":
			summary = true
		default:
			if strings.HasPrefix(f, "--") {
				return Args{}, usageError()
			}
			hint = append(hint, f)
		}
	}
	if transcript && summary {
		return Args{}, usageError()
	}
	return Args{Hint: strings.Join(hint, " "), Transcript: transcript, Summary: summary}, nil
}

func usageError() error {
	return fmt.Errorf("usage: /bug [--transcript | --summary] [description]")
}
