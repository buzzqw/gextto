package main

// state_save.go keeps the state file off the hot tick path.
//
// Writing the whole state — a json.MarshalIndent of every torrent and every feed
// item, measured at ~4.5 ms and 1.2 MB with a thousand torrents — while holding
// d.mu made every dirty queue tick a pause for the API, the pages and the
// qBittorrent clients polling them. Swarm counts, rotations and the 5% progress
// steps dirty the state on a large share of ticks, so that was not rare.
//
// Now a dirty tick only marks the state and wakes one saver goroutine
// (scheduleSaveLocked). The saver collects the signals of a short window, then
// marshals one snapshot under d.mu (the state cannot be read outside it) and
// writes the file outside the lock. Handlers that answer "saved" still call
// saveLocked, which writes synchronously: the change is on disk when it
// returns.
//
// Both paths take saveMu, so the temporary file has exactly one writer at a
// time, and every snapshot carries a sequence number taken under d.mu: a
// coalesced write that lost the race with a later synchronous save is dropped
// instead of putting older state on disk. close stops the saver and writes the
// final state, so the last change before a shutdown is always persisted; a
// crash can lose at most one debounce window of tick bookkeeping.

import (
	"encoding/json"
	"os"
	"time"
)

const (
	// stateSaveDebounce is how long the saver collects dirty signals before it
	// writes. A crash can lose at most this much tick bookkeeping (swarm
	// counts, rotations, progress steps), far less than the 60 s lifetime
	// flush, and a graceful close always writes the last state. It must stay
	// longer than the queue tick: a 300 ms window made the saver marshal the
	// whole state several times a second at 1000 torrents, more CPU than the
	// 3 s tick save it replaced. ten seconds keeps the marshal well under one
	// per tick and the crash window small.
	stateSaveDebounce = 10 * time.Second
	// stateSaveRetry is the pause before a failed write is tried again.
	stateSaveRetry = 5 * time.Second
)

// startStateSaver starts the coalescing writer. newDaemon calls it once, after
// every error path that must not leave a goroutine behind; it runs until
// stopStateSaver. A daemon with no state path (a bare test Daemon) has nothing
// to write and gets no goroutine.
func (d *Daemon) startStateSaver() {
	if d.stateWake != nil || d.opts.StatePath == "" {
		return
	}
	if d.stateDelay <= 0 {
		d.stateDelay = stateSaveDebounce
	}
	d.stateWake = make(chan struct{}, 1)
	d.stateQuit = make(chan struct{})
	d.stateDone = make(chan struct{})
	go d.runStateSaver()
}

// stopStateSaver ends the writer and waits for it to exit. close calls it
// before the final save: a snapshot taken before the last change must not reach
// the disk after it.
func (d *Daemon) stopStateSaver() {
	d.stateStopOnce.Do(func() {
		if d.stateQuit == nil {
			return
		}
		close(d.stateQuit)
		<-d.stateDone
	})
}

// scheduleSaveLocked marks the state dirty and asks the saver for a write. It
// never blocks and never touches the disk: the tick holds d.mu and must not
// wait on a file write. A bare Daemon with no saver (a test) falls back to the
// synchronous save.
func (d *Daemon) scheduleSaveLocked() {
	if d.opts.StatePath == "" {
		return
	}
	d.dirty = true
	if d.stateWake == nil {
		d.saveLocked()
		return
	}
	d.signalStateSaver()
}

// signalStateSaver wakes the saver without blocking. One pending signal is
// enough: the saver writes the whole state and coalesces everything queued
// behind it. It is a no-op on a bare Daemon that has no saver.
func (d *Daemon) signalStateSaver() {
	if d.stateWake == nil {
		return
	}
	select {
	case d.stateWake <- struct{}{}:
	default:
	}
}

// runStateSaver is the coalescing writer loop.
func (d *Daemon) runStateSaver() {
	defer close(d.stateDone)
	// retry is set after a failed write: the state was already taken off the
	// dirty flag, so the loop must snapshot again without a new signal.
	retry := false
	for {
		if retry {
			select {
			case <-d.stateQuit:
				return
			case <-time.After(stateSaveRetry):
			}
		} else {
			select {
			case <-d.stateQuit:
				return
			case <-d.stateWake:
			}
			// Coalesce: the dirty ticks that arrive during this window join the
			// write below instead of starting one each.
			timer := time.NewTimer(d.stateDelay)
			select {
			case <-d.stateQuit:
				timer.Stop()
				return
			case <-timer.C:
			}
			// A signal that arrived while waiting is already covered by the
			// snapshot below: leaving it would only cause a second identical
			// write.
			select {
			case <-d.stateWake:
			default:
			}
		}

		d.mu.Lock()
		if !d.dirty && !retry {
			d.mu.Unlock()
			continue
		}
		seq, data, err := d.snapshotStateLocked()
		d.mu.Unlock()
		if err == nil {
			err = d.writeState(seq, data)
		}
		retry = err != nil
	}
}

// snapshotStateLocked encodes the state and numbers the snapshot. The caller
// holds d.mu. dirty is cleared only once the encoding succeeded: a failed
// encoding keeps it set so the next pass tries again.
func (d *Daemon) snapshotStateLocked() (uint64, []byte, error) {
	d.state.BadTrackers = d.trackers.disabledSnapshot()
	data, err := json.MarshalIndent(d.state, "", "  ")
	if err != nil {
		logfError("cannot encode state: %v", err)
		return 0, nil, err
	}
	d.stateSeq++
	d.dirty = false
	return d.stateSeq, data, nil
}

// writeState writes one snapshot atomically. saveMu is what keeps the
// temporary file to a single writer; a snapshot older than the one already on
// disk is dropped, so a slow coalesced write can never undo a handler's
// synchronous save.
func (d *Daemon) writeState(seq uint64, data []byte) error {
	d.saveMu.Lock()
	defer d.saveMu.Unlock()
	if seq <= d.stateWritten {
		return nil
	}
	tmp := d.opts.StatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		// Never leave a partially written temporary behind.
		_ = os.Remove(tmp)
		logfError("cannot write state: %v", err)
		return err
	}
	if err := os.Rename(tmp, d.opts.StatePath); err != nil {
		// Never leave the temporary behind when the replace fails.
		_ = os.Remove(tmp)
		logfError("cannot replace state: %v", err)
		return err
	}
	d.stateWritten = seq
	d.stateWrites.Add(1)
	return nil
}
