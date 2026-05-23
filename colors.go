package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	colorAuto   = "auto"
	colorAlways = "always"
	colorNever  = "never"

	ansiReset        = "\x1b[0m"
	ansiDim          = "\x1b[2m"
	ansiRed          = "\x1b[31m"
	ansiGreen        = "\x1b[32m"
	ansiYellow       = "\x1b[33m"
	ansiCyan         = "\x1b[36m"
	ansiBrightGreen  = "\x1b[1;32m"
	ansiBrightYellow = "\x1b[1;33m"
	ansiBrightCyan   = "\x1b[1;36m"
)

type colors struct {
	enabled bool
}

func newColors(mode string, stream *os.File) (colors, error) {
	switch mode {
	case colorAuto:
		return colors{enabled: supportsColor(stream)}, nil
	case colorAlways:
		return colors{enabled: true}, nil
	case colorNever:
		return colors{}, nil
	default:
		return colors{}, fmt.Errorf("unknown color mode: %s", mode)
	}
}

func supportsColor(stream *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if colorEnvTruthy("CLICOLOR_FORCE") || colorEnvTruthy("FORCE_COLOR") {
		return true
	}
	if os.Getenv("CLICOLOR") == "0" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if stream == nil {
		return false
	}
	info, err := stream.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func colorEnvTruthy(key string) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	value = strings.ToLower(strings.TrimSpace(value))
	return value != "" && value != "0" && value != "false" && value != "no"
}

func (c colors) style(code, text string) string {
	if !c.enabled || text == "" {
		return text
	}
	return code + text + ansiReset
}

func (c colors) header(text string) string {
	return c.style(ansiBrightCyan, text)
}

func (c colors) dim(text string) string {
	return c.style(ansiDim, text)
}

func (c colors) red(text string) string {
	return c.style(ansiRed, text)
}

func (c colors) green(text string) string {
	return c.style(ansiGreen, text)
}

func (c colors) yellow(text string) string {
	return c.style(ansiYellow, text)
}

func (c colors) cyan(text string) string {
	return c.style(ansiCyan, text)
}

func (c colors) brightGreen(text string) string {
	return c.style(ansiBrightGreen, text)
}

func (c colors) brightYellow(text string) string {
	return c.style(ansiBrightYellow, text)
}

func (c colors) status(status string) string {
	switch status {
	case "ok":
		return c.green(status)
	case "update":
		return c.brightYellow(status)
	case "error":
		return c.red(status)
	case "skipped":
		return c.dim(status)
	default:
		return status
	}
}

func updateStatus(update Update) string {
	if update.Error != "" {
		return "error"
	}
	if update.SkipReason != "" {
		return "skipped"
	}
	if update.Update {
		return "update"
	}
	return "ok"
}
