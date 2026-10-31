package main

// postprocess.go runs the standalone completion actions: move the finished
// data to a library folder (optionally one subfolder per category) and/or run
// an external program with the torrent's details in the environment. Both are
// off by default; the command is never built from the torrent name, which
// travels in GX_NAME, so a hostile title cannot inject shell syntax.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// postProcessMoveKey is the folder completed torrents are moved to.
	postProcessMoveKey = "postprocess-move"
	// postProcessMoveCategoryKey puts each torrent in a subfolder named after
	// its category.
	postProcessMoveCategoryKey = "postprocess-move-category"
	// postProcessRunKey is the program run on completion, with its arguments
	// separated by spaces.
	postProcessRunKey = "postprocess-run"
)

// postProcessEnabled reports whether any completion action is configured.
func (d *Daemon) postProcessEnabled() bool {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		return false
	}
	return strings.TrimSpace(d.opts.Settings.Get(postProcessMoveKey, "")) != "" ||
		strings.TrimSpace(d.opts.Settings.Get(postProcessRunKey, "")) != ""
}

// runPostProcess performs the completion actions of one torrent. It runs in
// its own goroutine: moving can take minutes on a large pack and the command is
// external.
func (d *Daemon) runPostProcess(id string) {
	if d.opts.Settings == nil {
		return
	}
	d.mu.Lock()
	t, meta := d.findLocked(id)
	if t == nil || meta == nil {
		d.mu.Unlock()
		return
	}
	torrentID := t.ID()
	infoHash := t.InfoHash().String()
	stats := t.Stats()
	name, category, savePath := stats.Name, meta.Category, meta.SavePath
	total := stats.Bytes.Total
	d.mu.Unlock()
	if name == "" {
		name = id
	}

	if moveTo := strings.TrimSpace(d.opts.Settings.Get(postProcessMoveKey, "")); moveTo != "" {
		dest := moveTo
		if d.settingBool(postProcessMoveCategoryKey, false) && category != "" {
			dest = filepath.Join(moveTo, categoryDirName(category))
		}
		if err := d.move(torrentID, dest, false); err != nil {
			logfError("cannot move «%s» after the download: %v", name, err)
		} else {
			// The mover is asynchronous: wait for it before handing the final
			// path to the command, with a generous ceiling.
			for i := 0; i < 900; i++ {
				d.mu.Lock()
				moving := d.moving[torrentID]
				d.mu.Unlock()
				if !moving {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			d.mu.Lock()
			if current := d.state.Torrents[torrentID]; current != nil {
				savePath = current.SavePath
				if strings.HasPrefix(current.Error, "move failed:") {
					logfError("«%s» was not moved, the completion command sees the old folder", name)
				}
			}
			d.mu.Unlock()
		}
	}

	command := strings.TrimSpace(d.opts.Settings.Get(postProcessRunKey, ""))
	if command == "" {
		return
	}
	if err := runCompletionCommand(command, infoHash, name, savePath, category, total); err != nil {
		logfError("the completion command for «%s» failed: %v", name, err)
		return
	}
	logf("the completion command for «%s» finished", name)
}

// runCompletionCommand runs the operator's completion program and returns its
// combined output with the error. The torrent details go in the environment,
// never on the command line, so a hostile name cannot add arguments.
func runCompletionCommand(command, id, name, savePath, category string, total int64) error {
	args := strings.Fields(command)
	if len(args) == 0 {
		return nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(),
		"GX_HASH="+id,
		"GX_NAME="+name,
		"GX_PATH="+savePath,
		"GX_CATEGORY="+category,
		"GX_SIZE="+strconv.FormatInt(total, 10),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// categoryDirName turns a category into one safe folder name: separators and
// parent references cannot escape the completion folder.
func categoryDirName(category string) string {
	name := strings.TrimSpace(category)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return '-'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		return "category"
	}
	return name
}
