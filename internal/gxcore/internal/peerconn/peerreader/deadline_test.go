package peerreader

import (
	"net"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

// scriptConn replays one step per Read and counts deadline arms.
type scriptConn struct {
	net.Conn
	steps []func(b []byte) (int, error)
	reads int
	arms  int
	dl    time.Time
}

func (c *scriptConn) Read(b []byte) (int, error) {
	c.reads++
	if len(c.steps) == 0 {
		return 0, timeoutErr{}
	}
	s := c.steps[0]
	c.steps = c.steps[1:]
	return s(b)
}

func (c *scriptConn) SetReadDeadline(t time.Time) error {
	c.arms++
	c.dl = t
	return nil
}

// expire makes the armed deadline already passed, as if the window elapsed.
func (c *scriptConn) expire(d *deadlineReader) func(b []byte) (int, error) {
	return func(b []byte) (int, error) {
		d.deadline = time.Now().Add(-time.Millisecond)
		return 0, timeoutErr{}
	}
}

func TestDeadlineReaderDeadConnectionDoesNotSpin(t *testing.T) {
	// uTP reports a destroyed connection (ack timeout) as a net timeout on
	// every Read. Before the armed deadline that must end the read at once.
	c := &scriptConn{}
	d := &deadlineReader{conn: c, window: time.Minute}
	if err := d.arm(time.Minute); err != nil {
		t.Fatal(err)
	}
	d.progressed = true
	if _, err := d.Read(make([]byte, 4)); err == nil {
		t.Fatal("dead connection: want error")
	}
	if c.reads != 1 || c.arms != 1 {
		t.Fatalf("reads=%d arms=%d, want 1 and 1 (no retry loop)", c.reads, c.arms)
	}
}

func TestDeadlineReaderKeepsSlowPeer(t *testing.T) {
	c := &scriptConn{}
	d := &deadlineReader{conn: c, window: time.Minute}
	if err := d.arm(time.Minute); err != nil {
		t.Fatal(err)
	}
	c.steps = []func([]byte) (int, error){
		func(b []byte) (int, error) { b[0] = 1; return 1, nil },
		c.expire(d), // window passed after progress: re-arm and continue
		func(b []byte) (int, error) { b[0] = 2; return 1, nil },
	}
	buf := make([]byte, 1)
	for i := range 2 {
		if n, err := d.Read(buf); n != 1 || err != nil {
			t.Fatalf("read %d: n=%d err=%v", i, n, err)
		}
	}
	if c.arms != 2 {
		t.Fatalf("arms=%d, want 2 (initial + one re-arm)", c.arms)
	}
}

func TestDeadlineReaderDropsSilentPeer(t *testing.T) {
	c := &scriptConn{}
	d := &deadlineReader{conn: c, window: time.Minute}
	if err := d.arm(time.Minute); err != nil {
		t.Fatal(err)
	}
	c.steps = []func([]byte) (int, error){c.expire(d)}
	_, err := d.Read(make([]byte, 1))
	if nerr, ok := err.(net.Error); !ok || !nerr.Timeout() {
		t.Fatalf("silent peer: want timeout, got %v", err)
	}
	if c.arms != 1 {
		t.Fatalf("arms=%d, want 1", c.arms)
	}
}
