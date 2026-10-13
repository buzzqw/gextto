package upstream

// GEXTTO: our own uTP (BEP 29) code; see doc.go. Read/write deadlines: a deadline is a time plus an
// event that fires once it passes.

import (
	"time"
)

// deadline is a single read or write deadline.
type deadline struct {
	t      time.Time
	passed Event
	timer  *time.Timer
}

// set arms the deadline for t, clearing any previous one.
func (d *deadline) set(t time.Time) {
	d.t = t
	d.passed.Clear()
	if d.timer != nil {
		d.timer.Stop()
	}
	d.update()
}

// update rearms the timer, or marks the deadline passed when t is already due.
func (d *deadline) update() {
	if d.t.IsZero() {
		return
	}
	now := time.Now()
	if now.Before(d.t) {
		wait := d.t.Sub(now)
		if d.timer == nil {
			d.timer = time.AfterFunc(wait, d.callback)
		} else {
			d.timer.Reset(wait)
		}
		return
	}
	d.passed.Set()
}

func (d *deadline) callback() {
	mu.Lock()
	defer mu.Unlock()
	d.update()
}

// connDeadlines embeds a read and a write deadline for net.Conn.
type connDeadlines struct {
	read, write deadline
}

func (c *connDeadlines) SetDeadline(t time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	c.read.set(t)
	c.write.set(t)
	return nil
}

func (c *connDeadlines) SetReadDeadline(t time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	c.read.set(t)
	return nil
}

func (c *connDeadlines) SetWriteDeadline(t time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	c.write.set(t)
	return nil
}
