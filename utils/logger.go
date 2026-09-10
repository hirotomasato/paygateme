// Package utils — structured logger interface.
package utils

import (
	"fmt"
	"log"
	"os"
)

// Level is the log severity level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "???"
	}
}

// Logger is a minimal structured logger interface.
type Logger interface {
	Debug(msg string, meta map[string]any)
	Info(msg string, meta map[string]any)
	Warn(msg string, meta map[string]any)
	Error(msg string, meta map[string]any)
}

// NoopLogger is a logger that discards all messages.
var NoopLogger Logger = &noopLogger{}

type noopLogger struct{}

func (n *noopLogger) Debug(string, map[string]any) {}
func (n *noopLogger) Info(string, map[string]any)  {}
func (n *noopLogger) Warn(string, map[string]any)  {}
func (n *noopLogger) Error(string, map[string]any) {}

// ConsoleLogger is a console-backed logger that respects a minimum level.
type ConsoleLogger struct {
	minLevel Level
	prefix   string
}

// NewConsoleLogger creates a ConsoleLogger with the given minimum level.
func NewConsoleLogger(minLevel Level) *ConsoleLogger {
	return &ConsoleLogger{
		minLevel: minLevel,
		prefix:   "[paygateme]",
	}
}

func (c *ConsoleLogger) log(level Level, msg string, meta map[string]any) {
	if level < c.minLevel {
		return
	}
	line := fmt.Sprintf("%s %s %s", c.prefix, level, msg)
	if meta != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", line, meta)
	} else {
		fmt.Fprintln(os.Stderr, line)
	}
}

func (c *ConsoleLogger) Debug(msg string, meta map[string]any) {
	c.log(LevelDebug, msg, meta)
}

func (c *ConsoleLogger) Info(msg string, meta map[string]any) {
	// Info goes to stdout, not stderr.
	if c.minLevel > LevelInfo {
		return
	}
	line := fmt.Sprintf("%s %s %s", c.prefix, "INFO", msg)
	if meta != nil {
		fmt.Printf("%s %v\n", line, meta)
	} else {
		fmt.Println(line)
	}
}

func (c *ConsoleLogger) Warn(msg string, meta map[string]any) {
	c.log(LevelWarn, msg, meta)
}

func (c *ConsoleLogger) Error(msg string, meta map[string]any) {
	c.log(LevelError, msg, meta)
}

// StdLogger is a simple logger that writes to a standard library Logger.
type StdLogger struct {
	logger *log.Logger
	level  Level
}

// NewStdLogger creates a StdLogger with the given standard library Logger.
func NewStdLogger(l *log.Logger, level Level) *StdLogger {
	return &StdLogger{logger: l, level: level}
}

func (s *StdLogger) Debug(msg string, meta map[string]any) {}
func (s *StdLogger) Info(msg string, meta map[string]any)  {}
func (s *StdLogger) Warn(msg string, meta map[string]any)  {}
func (s *StdLogger) Error(msg string, meta map[string]any) {
	s.logger.Printf("[paygateme] %s %s %v", "ERROR", msg, meta)
}