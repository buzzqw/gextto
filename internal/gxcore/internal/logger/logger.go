// Package logger is the engine's logging facade. It re-exports the logging
// package so the engine packages share a single import.
package logger

import "github.com/buzzqw/gextto/internal/gxcore/logging"

type (
	Logger    = logging.Logger
	Handler   = logging.Handler
	Record    = logging.Record
	Level     = logging.Level
	Formatter = logging.Formatter
)

const (
	CRITICAL = logging.CRITICAL
	ERROR    = logging.ERROR
	WARNING  = logging.WARNING
	NOTICE   = logging.NOTICE
	INFO     = logging.INFO
	DEBUG    = logging.DEBUG
)

// New returns a new engine logger with the given name.
func New(name string) Logger { return logging.NewEngineLogger(name) }

// SetHandler changes the handler used by the engine loggers created afterwards.
func SetHandler(h Handler) { logging.SetEngineHandler(h) }

// SetDebug lowers the global engine handler to DEBUG.
func SetDebug() { logging.SetEngineDebug() }

// Disable discards the engine log records.
func Disable() { logging.DisableEngine() }
