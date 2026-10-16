package logger

import (
	"fmt"
	"io"
	"os"

	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/fatih/color"
)

var (
	output io.Writer = os.Stderr

	successColor = color.New(color.FgGreen)
	errorColor   = color.New(color.FgRed)
	infoColor    = color.New(color.FgCyan)
	warningColor = color.New(color.FgYellow)
	debugColor   = color.New(color.FgHiBlack)

	debugEnabled   = os.Getenv("DEBUG") != ""
	verboseEnabled = false
)

func SetOutput(w io.Writer) io.Writer {
	previous := output
	if w == nil {
		output = os.Stderr
		return previous
	}
	output = w
	return previous
}

func SetVerbose(verbose bool) {
	verboseEnabled = verbose
}

func Success(format string, args ...any) {
	successColor.Fprintf(output, format+"\n", safeArgs(args)...)
}

func Error(format string, args ...any) {
	errorColor.Fprintf(output, format+"\n", safeArgs(args)...)
}

func Info(format string, args ...any) {
	infoColor.Fprintf(output, format+"\n", safeArgs(args)...)
}

func Warning(format string, args ...any) {
	warningColor.Fprintf(output, format+"\n", safeArgs(args)...)
}

func Plain(format string, args ...any) {
	fmt.Fprintf(output, format+"\n", safeArgs(args)...)
}

func Debug(format string, args ...any) {
	if debugEnabled {
		debugColor.Fprintf(output, "[DEBUG] "+format+"\n", safeArgs(args)...)
	}
}

func Verbose(format string, args ...any) {
	if verboseEnabled {
		infoColor.Fprintf(output, format+"\n", safeArgs(args)...)
	}
}

func safeArgs(args []any) []any {
	safe := make([]any, len(args))
	for i, arg := range args {
		switch value := arg.(type) {
		case string:
			safe[i] = terminal.Sanitize(value)
		case error:
			safe[i] = terminal.Sanitize(value.Error())
		case fmt.Stringer:
			safe[i] = terminal.Sanitize(value.String())
		default:
			safe[i] = arg
		}
	}
	return safe
}
