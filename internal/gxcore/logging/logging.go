// Package logging is the gx-core engine's leveled logger. It is a small,
// self-contained logger: records carry a level, a name and the caller's source
// position, and a pluggable handler decides where they go.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-isatty"
)

// Level orders messages from most to least severe. A handler set at a level
// forwards the records whose level is not greater than it, so DEBUG shows
// everything and WARNING hides INFO and DEBUG.
type Level int

const (
	CRITICAL Level = iota
	ERROR
	WARNING
	NOTICE
	INFO
	DEBUG
)

// String returns the upper-case name of the level.
func (l Level) String() string {
	switch l {
	case CRITICAL:
		return "CRITICAL"
	case ERROR:
		return "ERROR"
	case WARNING:
		return "WARNING"
	case NOTICE:
		return "NOTICE"
	case INFO:
		return "INFO"
	case DEBUG:
		return "DEBUG"
	default:
		return "UNKNOWN"
	}
}

// Record is a single log message with everything needed to render it.
type Record struct {
	Message     string    // Formatted log message.
	LoggerName  string    // Name of the logger module.
	Level       Level     // Level of the record.
	Time        time.Time // Time of the record (local time).
	Filename    string    // Source file of the log call.
	Line        int       // Line number in that file.
	ProcessID   int       // PID.
	ProcessName string    // Name of the process.
}

// Formatter renders a record to a line of text.
type Formatter interface {
	Format(*Record) string
}

// Handler consumes records. It owns the level filter and the output.
type Handler interface {
	SetFormatter(Formatter)
	SetLevel(Level)
	Handle(*Record)
	Close() error
}

// Logger emits records at the different levels. A new one is created with
// NewLogger.
type Logger interface {
	SetLevel(Level)
	SetHandler(Handler)
	SetCallDepth(int)

	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Fatalln(args ...interface{})
	Panic(args ...interface{})
	Panicf(format string, args ...interface{})
	Panicln(args ...interface{})

	Critical(args ...interface{})
	Criticalf(format string, args ...interface{})
	Criticalln(args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Errorln(args ...interface{})
	Warning(args ...interface{})
	Warningf(format string, args ...interface{})
	Warningln(args ...interface{})
	Notice(args ...interface{})
	Noticef(format string, args ...interface{})
	Noticeln(args ...interface{})
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Infoln(args ...interface{})
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Debugln(args ...interface{})
}

var (
	procName = filepath.Base(os.Args[0])
	pid      = os.Getpid()

	// DefaultLogger, DefaultHandler and DefaultLevel are used by the
	// package-level logging functions.
	DefaultLogger    Logger    = NewLogger(procName)
	DefaultLevel     Level     = INFO
	DefaultHandler   Handler   = NewFileHandler(os.Stderr)
	DefaultFormatter Formatter = plainFormatter{}
)

// SetLevel changes the level of DefaultLogger and DefaultHandler.
func SetLevel(l Level) {
	DefaultLogger.SetLevel(l)
	DefaultHandler.SetLevel(l)
}

func Fatal(args ...interface{})                 { DefaultLogger.Fatal(args...) }
func Fatalf(format string, args ...interface{}) { DefaultLogger.Fatalf(format, args...) }
func Fatalln(args ...interface{})               { DefaultLogger.Fatalln(args...) }
func Panic(args ...interface{})                 { DefaultLogger.Panic(args...) }
func Panicf(format string, args ...interface{}) { DefaultLogger.Panicf(format, args...) }
func Panicln(args ...interface{})               { DefaultLogger.Panicln(args...) }
func Critical(args ...interface{})              { DefaultLogger.Critical(args...) }
func Criticalf(format string, args ...interface{}) {
	DefaultLogger.Criticalf(format, args...)
}
func Criticalln(args ...interface{}) { DefaultLogger.Criticalln(args...) }
func Error(args ...interface{})      { DefaultLogger.Error(args...) }
func Errorf(format string, args ...interface{}) {
	DefaultLogger.Errorf(format, args...)
}
func Errorln(args ...interface{}) { DefaultLogger.Errorln(args...) }
func Warning(args ...interface{}) { DefaultLogger.Warning(args...) }
func Warningf(format string, args ...interface{}) {
	DefaultLogger.Warningf(format, args...)
}
func Warningln(args ...interface{}) { DefaultLogger.Warningln(args...) }
func Notice(args ...interface{})    { DefaultLogger.Notice(args...) }
func Noticef(format string, args ...interface{}) {
	DefaultLogger.Noticef(format, args...)
}
func Noticeln(args ...interface{}) { DefaultLogger.Noticeln(args...) }
func Info(args ...interface{})     { DefaultLogger.Info(args...) }
func Infof(format string, args ...interface{}) {
	DefaultLogger.Infof(format, args...)
}
func Infoln(args ...interface{}) { DefaultLogger.Infoln(args...) }
func Debug(args ...interface{})  { DefaultLogger.Debug(args...) }
func Debugf(format string, args ...interface{}) {
	DefaultLogger.Debugf(format, args...)
}
func Debugln(args ...interface{}) { DefaultLogger.Debugln(args...) }

// logger is the default Logger implementation.
type logger struct {
	name      string
	level     Level
	handler   Handler
	callDepth int
	// engine marks a logger created by NewEngineLogger, whose threshold is the
	// shared engine level rather than the logger's own.
	engine bool
}

// NewLogger returns a new logger that writes to DefaultHandler at DefaultLevel.
func NewLogger(name string) Logger {
	return &logger{name: name, level: DefaultLevel, handler: DefaultHandler}
}

func (l *logger) SetLevel(level Level) { l.level = level }
func (l *logger) SetHandler(b Handler) { l.handler = b }
func (l *logger) SetCallDepth(n int)   { l.callDepth = n }

func (l *logger) emit(level Level, message string) {
	if l.engine {
		// Drop records the engine handler would filter out anyway, before
		// building the record, so hot paths stay cheap when the daemon runs at
		// WARNING. The threshold mirrors the installed handler.
		if level > Level(engineLevel.Load()) {
			return
		}
	} else if level > l.level {
		return
	}
	_, file, line, ok := runtime.Caller(l.callDepth + 2)
	if !ok {
		file, line = "???", 0
	}
	l.handler.Handle(&Record{
		Message:     message,
		LoggerName:  l.name,
		Level:       level,
		Time:        time.Now(),
		Filename:    file,
		Line:        line,
		ProcessName: procName,
		ProcessID:   pid,
	})
}

func (l *logger) Fatal(args ...interface{}) { l.emit(CRITICAL, fmt.Sprint(args...)); os.Exit(1) }
func (l *logger) Fatalf(format string, args ...interface{}) {
	l.emit(CRITICAL, fmt.Sprintf(format, args...))
	os.Exit(1)
}
func (l *logger) Fatalln(args ...interface{}) {
	l.emit(CRITICAL, fmt.Sprintln(args...))
	os.Exit(1)
}
func (l *logger) Panic(args ...interface{}) {
	l.emit(CRITICAL, fmt.Sprint(args...))
	panic(fmt.Sprint(args...))
}
func (l *logger) Panicf(format string, args ...interface{}) {
	l.emit(CRITICAL, fmt.Sprintf(format, args...))
	panic(fmt.Sprintf(format, args...))
}
func (l *logger) Panicln(args ...interface{}) {
	l.emit(CRITICAL, fmt.Sprintln(args...))
	panic(fmt.Sprintln(args...))
}

func (l *logger) Critical(args ...interface{}) { l.emit(CRITICAL, fmt.Sprint(args...)) }
func (l *logger) Criticalf(format string, args ...interface{}) {
	l.emit(CRITICAL, fmt.Sprintf(format, args...))
}
func (l *logger) Criticalln(args ...interface{}) { l.emit(CRITICAL, fmt.Sprintln(args...)) }
func (l *logger) Error(args ...interface{})      { l.emit(ERROR, fmt.Sprint(args...)) }
func (l *logger) Errorf(format string, args ...interface{}) {
	l.emit(ERROR, fmt.Sprintf(format, args...))
}
func (l *logger) Errorln(args ...interface{}) { l.emit(ERROR, fmt.Sprintln(args...)) }
func (l *logger) Warning(args ...interface{}) { l.emit(WARNING, fmt.Sprint(args...)) }
func (l *logger) Warningf(format string, args ...interface{}) {
	l.emit(WARNING, fmt.Sprintf(format, args...))
}
func (l *logger) Warningln(args ...interface{}) { l.emit(WARNING, fmt.Sprintln(args...)) }
func (l *logger) Notice(args ...interface{})    { l.emit(NOTICE, fmt.Sprint(args...)) }
func (l *logger) Noticef(format string, args ...interface{}) {
	l.emit(NOTICE, fmt.Sprintf(format, args...))
}
func (l *logger) Noticeln(args ...interface{}) { l.emit(NOTICE, fmt.Sprintln(args...)) }
func (l *logger) Info(args ...interface{})     { l.emit(INFO, fmt.Sprint(args...)) }
func (l *logger) Infof(format string, args ...interface{}) {
	l.emit(INFO, fmt.Sprintf(format, args...))
}
func (l *logger) Infoln(args ...interface{}) { l.emit(INFO, fmt.Sprintln(args...)) }
func (l *logger) Debug(args ...interface{})  { l.emit(DEBUG, fmt.Sprint(args...)) }
func (l *logger) Debugf(format string, args ...interface{}) {
	l.emit(DEBUG, fmt.Sprintf(format, args...))
}
func (l *logger) Debugln(args ...interface{}) { l.emit(DEBUG, fmt.Sprintln(args...)) }

// engineHandler is the handler shared by the loggers the engine creates. It is
// guarded because a new session may replace it while other sessions' goroutines
// create loggers.
var (
	engineMu      sync.RWMutex
	engineHandler Handler = NewFileHandler(os.Stderr)
	engineLevel   atomic.Int32
)

func init() {
	// The package-level logging functions add one frame, so the default logger
	// must skip it to report the real call site.
	DefaultLogger.SetCallDepth(1)
	engineHandler.SetFormatter(engineFormatter{})
	engineLevel.Store(int32(DEBUG))
}

// SetEngineHandler changes the handler used by the engine loggers created
// afterwards.
func SetEngineHandler(h Handler) {
	h.SetFormatter(engineFormatter{})
	engineMu.Lock()
	engineHandler = h
	engineMu.Unlock()
}

// SetEngineLevel sets the threshold above which the engine loggers drop a
// record before building it. It must mirror the level of the installed
// handler: the handler would filter those records out anyway, so dropping them
// earlier only saves formatting and allocation on hot paths. Records destined
// to a counting or demoting wrapper are at ERROR/WARNING and stay above the
// usual WARNING threshold, so nothing observable is lost.
func SetEngineLevel(l Level) { engineLevel.Store(int32(l)) }

// SetEngineDebug lowers the engine handler to DEBUG.
func SetEngineDebug() {
	engineMu.RLock()
	engineHandler.SetLevel(DEBUG)
	engineMu.RUnlock()
	engineLevel.Store(int32(DEBUG))
}

// DisableEngine discards engine log records.
func DisableEngine() {
	SetEngineHandler(NewWriterHandler(io.Discard))
	engineLevel.Store(int32(DEBUG))
}

// NewEngineLogger returns a logger that forwards every record to the current
// engine handler.
func NewEngineLogger(name string) Logger {
	l := &logger{name: name, level: DEBUG, engine: true}
	engineMu.RLock()
	l.handler = engineHandler
	engineMu.RUnlock()
	return l
}

type plainFormatter struct{}

// Format outputs a message like:
//
//	2014-02-28 18:15:57 [example] INFO     something happened
func (plainFormatter) Format(rec *Record) string {
	return fmt.Sprintf("%s [%s] %-8s %s", fmt.Sprint(rec.Time)[:19], rec.LoggerName, rec.Level, rec.Message)
}

type engineFormatter struct{}

// Format outputs a message like:
//
//	2014-02-28 18:15:57 DEBUG    [example] file.go:12 something happened
func (engineFormatter) Format(rec *Record) string {
	return fmt.Sprintf("%s %-8s [%s] %-8s %s",
		fmt.Sprint(rec.Time)[:19],
		rec.Level,
		rec.LoggerName,
		filepath.Base(rec.Filename)+":"+strconv.Itoa(rec.Line),
		rec.Message)
}

// BaseHandler keeps the shared level filter and formatter of a handler.
type BaseHandler struct {
	level     Level
	formatter Formatter
}

// NewBaseHandler returns a handler at DefaultLevel with DefaultFormatter.
func NewBaseHandler() *BaseHandler {
	return &BaseHandler{level: DefaultLevel, formatter: DefaultFormatter}
}

// SetLevel sets the maximum level this handler forwards.
func (h *BaseHandler) SetLevel(l Level) { h.level = l }

// SetFormatter sets the formatter this handler uses.
func (h *BaseHandler) SetFormatter(f Formatter) { h.formatter = f }

// filterAndFormat returns the formatted record, or an empty string when the
// record is below the handler level.
func (h *BaseHandler) filterAndFormat(rec *Record) string {
	if rec.Level > h.level {
		return ""
	}
	return h.formatter.Format(rec)
}

// WriterHandler writes log records to an io.Writer.
type WriterHandler struct {
	*BaseHandler
	w io.Writer
	m sync.Mutex
}

// NewWriterHandler returns a handler that writes to w.
func NewWriterHandler(w io.Writer) *WriterHandler {
	return &WriterHandler{BaseHandler: NewBaseHandler(), w: w}
}

// Handle writes the record, adding a trailing newline when the formatter did
// not.
func (h *WriterHandler) Handle(rec *Record) {
	message := h.filterAndFormat(rec)
	if message == "" {
		return
	}
	if message[len(message)-1] != '\n' {
		message += "\n"
	}
	h.m.Lock()
	fmt.Fprint(h.w, message)
	h.m.Unlock()
}

// Close closes the writer when it is an io.Closer.
func (h *WriterHandler) Close() error {
	if c, ok := h.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// Color is a terminal color code.
type Color int

// Colors for the log levels.
const (
	BLACK Color = iota + 30
	RED
	GREEN
	YELLOW
	BLUE
	MAGENTA
	CYAN
	WHITE
	NOCOLOR Color = -1
)

// LevelColors maps each level to the color a terminal handler uses.
var LevelColors = map[Level]Color{
	CRITICAL: MAGENTA,
	ERROR:    RED,
	WARNING:  YELLOW,
	NOTICE:   GREEN,
	INFO:     NOCOLOR,
	DEBUG:    BLUE,
}

// FileHandler writes log records to an *os.File, coloring them when the file
// is a terminal.
type FileHandler struct {
	*BaseHandler
	f     *os.File
	m     sync.Mutex
	color bool
}

// NewFileHandler returns a handler that writes to f, colored when f is a
// terminal.
func NewFileHandler(f *os.File) *FileHandler {
	return &FileHandler{BaseHandler: NewBaseHandler(), f: f, color: isatty.IsTerminal(f.Fd())}
}

// Handle writes the record, coloring it when the file is a terminal.
func (h *FileHandler) Handle(rec *Record) {
	message := h.filterAndFormat(rec)
	if message == "" {
		return
	}
	if message[len(message)-1] != '\n' {
		message += "\n"
	}
	if h.color {
		if c := LevelColors[rec.Level]; c != NOCOLOR {
			message = fmt.Sprintf("\033[%dm%s\033[0m", c, message)
		}
	}
	h.m.Lock()
	fmt.Fprint(h.f, message)
	h.m.Unlock()
}

// Close closes the underlying file.
func (h *FileHandler) Close() error {
	return h.f.Close()
}
