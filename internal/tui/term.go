package tui

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

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
	// Alternate screen buffer, hide cursor, clear, bracketed paste on (so a
	// pasted magnet arrives as one block instead of a stream of commands).
	// Autowrap goes off too: if a row were ever one cell wider than computed
	// (an odd emoji), the terminal clips it instead of wrapping and pushing
	// every row below it, footer included, out of place.
	_, _ = io.WriteString(t.out, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H\x1b[?2004h\x1b[?7l")
	return nil
}

func (t *terminal) leave() {
	if !t.active {
		return
	}
	_, _ = io.WriteString(t.out, "\x1b[?7h\x1b[?2004l\x1b[0m\x1b[?25h\x1b[?1049l")
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

// frameRenderer writes frames incrementally, because over SSH every byte
// crosses the network:
//   - rows equal to the previous frame are not sent at all;
//   - a changed row with an unchanged beginning (a speed, a percentage) is
//     rewritten only from the first changed column;
//   - when a region moved up (the log follows new lines) the terminal scrolls
//     it itself and only the new rows are sent.
//
// Each frame is a synchronized update (DEC mode 2026) so the terminal shows
// it at once instead of tearing; terminals that do not know it ignore it.
type frameRenderer struct {
	out    io.Writer
	ascii  bool
	rows   []renderedRow
	width  int
	height int
	cursor string
	valid  bool
}

// renderedRow is a row as the terminal shows it: final text and SGR style.
type renderedRow struct {
	text  string
	style string
}

func newFrameRenderer(out io.Writer, ascii bool) *frameRenderer {
	return &frameRenderer{out: out, ascii: ascii}
}

// invalidate forces the next draw to clear the screen and repaint every row
// (after a resize, Ctrl-L, or anything else that may have dirtied the screen).
func (r *frameRenderer) invalidate() { r.valid = false }

// draw sends the difference between screen and the previous frame and
// returns how many bytes it wrote.
func (r *frameRenderer) draw(screen Screen, width, height int) int {
	rows := make([]renderedRow, len(screen.Lines))
	for index, line := range screen.Lines {
		rows[index] = r.prepare(line, screen)
	}
	var body strings.Builder
	full := !r.valid || width != r.width || height != r.height
	if full {
		body.WriteString("\x1b[0m\x1b[H\x1b[2J")
		r.rows = r.rows[:0]
	} else {
		r.scroll(&body, rows)
	}
	for index, row := range rows {
		if index >= len(r.rows) {
			if row.text != "" {
				writeRow(&body, index, 0, row, true)
			}
			r.rows = append(r.rows, row)
			continue
		}
		old := r.rows[index]
		if old == row {
			continue
		}
		r.rows[index] = row
		if row.text == "" {
			fmt.Fprintf(&body, "\x1b[%d;1H\x1b[K", index+1)
			continue
		}
		prefix := 0
		if old.style == row.style {
			prefix = commonPrefix(old.text, row.text)
		}
		clear := StringWidth(row.text) < StringWidth(old.text)
		if prefix > 0 {
			writeRow(&body, index, prefix, row, clear)
		} else {
			writeRow(&body, index, 0, row, clear || old.style != row.style)
		}
	}
	for index := len(rows); index < len(r.rows); index++ {
		if r.rows[index].text != "" {
			fmt.Fprintf(&body, "\x1b[%d;1H\x1b[K", index+1)
		}
	}
	r.rows = r.rows[:min(len(r.rows), len(rows))]
	cursor := "\x1b[?25l"
	if screen.CursorVisible {
		cursor = fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", screen.CursorRow+1, screen.CursorCol+1)
	}
	if body.Len() == 0 && cursor == r.cursor {
		return 0
	}
	if body.Len() == 0 {
		_, _ = io.WriteString(r.out, cursor)
		r.cursor = cursor
		return len(cursor)
	}
	r.valid, r.width, r.height, r.cursor = true, width, height, cursor
	frame := "\x1b[?2026h" + body.String() + cursor + "\x1b[?2026l"
	_, _ = io.WriteString(r.out, frame)
	return len(frame)
}

// writeRow writes row from byte offset start (a rune boundary, at the cell
// column of row.text[:start]); clear erases what an older, longer row left.
func writeRow(body *strings.Builder, index, start int, row renderedRow, clear bool) {
	column := 1
	if start > 0 {
		column += StringWidth(row.text[:start])
	}
	fmt.Fprintf(body, "\x1b[%d;%dH", index+1, column)
	if row.style != "" {
		body.WriteString(row.style)
	}
	body.WriteString(row.text[start:])
	if row.style != "" {
		body.WriteString("\x1b[0m")
	}
	if clear {
		body.WriteString("\x1b[K")
	}
}

// commonPrefix returns the byte length of the common rune prefix, backing off
// so the rewrite never starts on a zero-width rune (it would combine with the
// cell before it) and is worth the cursor movement.
func commonPrefix(a, b string) int {
	length := 0
	for length < len(a) && length < len(b) {
		ra, size := utf8.DecodeRuneInString(a[length:])
		rb, _ := utf8.DecodeRuneInString(b[length:])
		if ra != rb {
			break
		}
		length += size
	}
	for length > 0 {
		next, _ := utf8.DecodeRuneInString(b[length:])
		if length == len(b) || RuneWidth(next) > 0 {
			break
		}
		_, size := utf8.DecodeLastRuneInString(b[:length])
		length -= size
	}
	if length < 8 {
		return 0
	}
	return length
}

// scroll looks for a block of rows that moved up by k rows and, when it is
// worth it, scrolls that region in the terminal so the diff only has to send
// the k rows that appeared. It updates r.rows to what the terminal shows.
func (r *frameRenderer) scroll(body *strings.Builder, rows []renderedRow) {
	count := min(len(r.rows), len(rows))
	last := -1
	for index := count - 1; index >= 0; index-- {
		if r.rows[index] != rows[index] {
			last = index
			break
		}
	}
	if last < 4 {
		return
	}
	bestShift, bestStart, bestRun := 0, 0, 0
	for shift := 1; shift <= min(last/2, 16); shift++ {
		start := last - shift
		for start >= 0 && r.rows[start+shift] == rows[start] {
			start--
		}
		start++
		if run := last - shift - start + 1; run > bestRun {
			bestShift, bestStart, bestRun = shift, start, run
		}
	}
	if bestRun < 4 {
		return
	}
	top, bottom := bestStart, last
	// Scroll region, cursor on its bottom row, one line feed per row moved
	// (the terminal is in raw mode, so LF does not imply CR), then reset.
	fmt.Fprintf(body, "\x1b[%d;%dr\x1b[%d;1H%s\x1b[r", top+1, bottom+1, bottom+1, strings.Repeat("\n", bestShift))
	copy(r.rows[top:bottom+1-bestShift], r.rows[top+bestShift:bottom+1])
	for index := bottom + 1 - bestShift; index <= bottom; index++ {
		r.rows[index] = renderedRow{}
	}
}

func (r *frameRenderer) prepare(line Line, screen Screen) renderedRow {
	text := Sanitize(line.Text)
	if r.ascii {
		text = ASCIIFold(text)
	}
	if !screen.ColorsEnabled || text == "" {
		return renderedRow{text: text}
	}
	return renderedRow{text: text, style: styleCode(line.Style, screen.HighContrast)}
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

// writeOSC52 asks the terminal to copy value to its clipboard; over SSH this
// reaches the clipboard of the machine the user sits at. Inside tmux or GNU
// screen the sequence is also sent wrapped for passthrough, because the
// multiplexer would otherwise swallow it (tmux forwards the plain form only
// with `set -g set-clipboard on`). Terminals without OSC52 ignore it.
func writeOSC52(w io.Writer, value string, getenv func(string) string) error {
	sequence := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(value)) + "\x07"
	payload := sequence
	switch {
	case getenv("TMUX") != "":
		payload += "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
	case getenv("STY") != "":
		payload = "\x1bP" + sequence + "\x1b\\"
	}
	_, err := io.WriteString(w, payload)
	return err
}
