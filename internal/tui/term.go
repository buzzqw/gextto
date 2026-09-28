package tui

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// terminal owns the raw-mode state and the alternate screen buffer.
type terminal struct {
	in       *os.File
	out      io.Writer
	original *unix.Termios
	active   bool
}

func newTerminal(in *os.File, out io.Writer) *terminal {
	return &terminal{in: in, out: out}
}

func (t *terminal) enter() error {
	fd := int(t.in.Fd())
	original, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	raw := *original
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 1
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return err
	}
	t.original = original
	t.active = true
	// Alternate screen buffer, hide cursor, clear.
	_, _ = io.WriteString(t.out, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")
	return nil
}

func (t *terminal) leave() {
	if !t.active {
		return
	}
	_, _ = io.WriteString(t.out, "\x1b[?25h\x1b[?1049l")
	if t.original != nil {
		_ = unix.IoctlSetTermios(int(t.in.Fd()), unix.TCSETS, t.original)
	}
	t.active = false
}

func (t *terminal) size() (width, height int) {
	ws, err := unix.IoctlGetWinsize(int(t.in.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

// renderANSIConvert writes a frame using the alternate screen.
func renderANSIConvert(w io.Writer, screen Screen) {
	var builder strings.Builder
	builder.WriteString("\x1b[H")
	for index, line := range screen.Lines {
		builder.WriteString("\x1b[2K")
		if screen.ColorsEnabled {
			builder.WriteString(styleCode(line.Style, screen.HighContrast))
		}
		builder.WriteString(line.Text)
		if screen.ColorsEnabled {
			builder.WriteString("\x1b[0m")
		}
		if index < len(screen.Lines)-1 {
			builder.WriteString("\r\n")
		}
	}
	if screen.CursorVisible {
		builder.WriteString("\x1b[?25h")
		builder.WriteString(fmt.Sprintf("\x1b[%d;%dH", screen.CursorRow+1, screen.CursorCol+1))
	} else {
		builder.WriteString("\x1b[?25l")
	}
	_, _ = io.WriteString(w, builder.String())
}

func styleCode(style Style, highContrast bool) string {
	if highContrast {
		switch style {
		case StyleMuted:
			return "\x1b[1;37m"
		case StyleHeader:
			return "\x1b[1;97;44m"
		case StyleOK:
			return "\x1b[1;92m"
		case StyleWarn:
			return "\x1b[1;93m"
		case StyleErr:
			return "\x1b[1;91m"
		case StyleSelected:
			return "\x1b[1;7m"
		}
	}
	switch style {
	case StyleMuted:
		return "\x1b[2m"
	case StyleHeader:
		return "\x1b[1;36m"
	case StyleOK:
		return "\x1b[32m"
	case StyleWarn:
		return "\x1b[33m"
	case StyleErr:
		return "\x1b[31m"
	case StyleSelected:
		return "\x1b[7m"
	default:
		return ""
	}
}

// writeOSC52 asks the terminal to copy value to its clipboard. Terminals that
// do not support OSC52 simply ignore the sequence; the TUI still reports that
// a copy was requested instead of invoking a desktop-specific utility.
func writeOSC52(w io.Writer, value string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	_, err := io.WriteString(w, "\x1b]52;c;"+encoded+"\x07")
	return err
}
