package transport

// GEXTTO: our own uTP (BEP 29) code; see doc.go. Read/write deadlines: a deadline is a time plus an
// event that fires once it passes.

import (
	"sync"
	"time"
)

// deadline is a single read or write deadline. Its owner is the lock guarding
// the deadline: a Conn's deadlines use the Conn's mu, a Socket's use socketMu.
type deadline struct {
	t      time.Time
	passed Event
	timer  *time.Timer
	lock   sync.Locker
}

// setOwner binds the deadline to the lock protecting its owner. It must be
// called before the owner becomes visible to other goroutines.
func (d *deadline) setOwner(lock sync.Locker) { d.lock = lock }

// set arms the deadline for t, clearing any previous one. The caller holds
// d.lock.
func (d *deadline) set(t time.Time) {
	d.t = t
	d.passed.Clear()
	if d.timer != nil {
		d.timer.Stop()
	}
	d.update()
}

// update rearms the timer, or marks the deadline passed when t is already due.
// The caller holds d.lock.
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
	d.lock.Lock()
	defer d.lock.Unlock()
	d.update()
}

// connDeadlines embeds a read and a write deadline for net.Conn. lock is the
// owner's lock, shared by both deadlines; setOwner installs it.
type connDeadlines struct {
	read, write deadline
	lock        sync.Locker
}

func (c *connDeadlines) setOwner(lock sync.Locker) {
	c.lock = lock
	c.read.setOwner(lock)
	c.write.setOwner(lock)
}

func (c *connDeadlines) SetDeadline(t time.Time) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.read.set(t)
	c.write.set(t)
	return nil
}

func (c *connDeadlines) SetReadDeadline(t time.Time) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.read.set(t)
	return nil
}

func (c *connDeadlines) SetWriteDeadline(t time.Time) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.write.set(t)
	return nil
}
