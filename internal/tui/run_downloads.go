package tui

import (
	"context"
	"errors"
)

// errSkipped marks a list that was not reloaded.
var errSkipped = errors.New("skipped")

// performTorrentAction runs the bulk, tag, history and per-torrent actions.
// It reports whether action was one of them.
func performTorrentAction(ctx context.Context, client *Client, tr *Translator, action Action) (actionResult, bool) {
	result := actionResult{}
	fail := func(err error) (actionResult, bool) {
		result.message = tr.Format("msg.actionfailed", action.Domain, err)
		return result, true
	}
	// reloadDetail refreshes the open torrent and, for trackers and files,
	// the list being edited.
	reloadDetail := func(message string, kind string) (actionResult, bool) {
		detail, detailErr := client.TorrentDetail(ctx, action.Hash)
		var items []map[string]any
		var itemsErr error = errSkipped
		if kind != "" {
			items, itemsErr = client.TorrentCollection(ctx, action.Hash, kind)
		}
		result.apply = func(m *Model) {
			if m.Detail == nil || m.Detail.Torrent.Hash != action.Hash {
				return
			}
			if detailErr == nil {
				view, selected := m.DetailView, m.DetailSelected
				m.Detail = &detail
				m.DetailView, m.DetailSelected = view, selected
			}
			if itemsErr == nil {
				m.SetDetailItems(items)
			}
		}
		result.message = message
		result.refresh = true
		return result, true
	}

	switch action.Kind {
	case ActionBulk:
		done := 0
		for _, hash := range action.Hashes {
			var err error
			switch action.Domain {
			case "pause":
				err = client.Pause(ctx, hash)
			case "resume":
				err = client.Resume(ctx, hash)
			case "recheck":
				err = client.Recheck(ctx, hash)
			case "remove", "removefiles":
				err = client.Remove(ctx, hash, action.Domain == "removefiles", false)
			}
			if err == nil {
				done++
			}
		}
		result.apply = func(m *Model) { m.Marked = nil }
		result.message = tr.Format("msg.bulkdone", tr.T("bulk."+action.Domain), done, len(action.Hashes))
		result.refresh = true
	case ActionSetTag:
		done := 0
		var lastErr error
		for index, hash := range action.Hashes {
			if err := client.SetTorrentTag(ctx, hash, action.Text, action.Flag && index == 0); err != nil {
				lastErr = err
				continue
			}
			done++
		}
		if done == 0 && lastErr != nil {
			action.Domain = "tag"
			return fail(lastErr)
		}
		tags, tagsErr := client.TorrentTags(ctx)
		catalog, _ := client.DownloadTags(ctx)
		result.apply = func(m *Model) {
			m.Marked = nil
			if tagsErr == nil {
				m.SetTorrentTags(tags, catalog)
			}
		}
		if action.Text == "" {
			result.message = tr.Format("msg.tagremoved", done)
		} else {
			result.message = tr.Format("msg.tagset", action.Text, done)
		}
	case ActionToggleAutoRemove:
		value := "false"
		if action.Flag {
			value = "true"
		}
		if err := client.SaveSetting(ctx, "auto_remove_completed", value); err != nil {
			action.Domain = "auto-remove"
			return fail(err)
		}
		if policy, err := client.SpeedPolicy(ctx); err == nil {
			result.apply = func(m *Model) { m.SetSpeedPolicy(policy) }
		}
		if action.Flag {
			result.message = tr.T("msg.autoremoveon")
		} else {
			result.message = tr.T("msg.autoremoveoff")
		}
	case ActionLoadHistory:
		items, total, err := client.History(ctx, action.Text)
		if err != nil {
			action.Domain = "history"
			return fail(err)
		}
		result.apply = func(m *Model) {
			m.History, m.HistoryTotal = items, total
			m.HistorySelected = min(m.HistorySelected, max(0, len(items)-1))
		}
		result.clearMessage = true
	case ActionTorrentLimits:
		if err := client.SetTorrentLimits(ctx, action.Hash, kibToBytes(action.DL), kibToBytes(action.UL), action.Ratio, action.Days); err != nil {
			action.Domain = "limits"
			return fail(err)
		}
		return reloadDetail(tr.Format("msg.torrentlimits", kibLabel(tr, action.DL), kibLabel(tr, action.UL)), "")
	case ActionMoveStorage:
		if err := client.MoveStorage(ctx, action.Hash, action.Text); err != nil {
			action.Domain = "storage"
			return fail(err)
		}
		return reloadDetail(tr.Format("msg.moving", action.Text), "")
	case ActionMarkFailed:
		if err := client.MarkFailed(ctx, action.Hash); err != nil {
			action.Domain = "mark-failed"
			return fail(err)
		}
		result.apply = func(m *Model) {
			if m.Detail != nil && m.Detail.Torrent.Hash == action.Hash {
				m.Detail = nil
			}
		}
		result.message = tr.T("msg.markedfailed")
		result.refresh = true
	case ActionSuperSeeding:
		if err := client.SetSuperSeeding(ctx, action.Hash, action.Flag); err != nil {
			action.Domain = "super-seeding"
			return fail(err)
		}
		message := tr.T("msg.superseedingoff")
		if action.Flag {
			message = tr.T("msg.superseedingon")
		}
		return reloadDetail(message, "")
	case ActionFilePriorities:
		if err := client.SetFilePriorities(ctx, action.Hash, action.Priorities); err != nil {
			action.Domain = "files"
			result, handled := fail(err)
			// Reload to undo the optimistic change.
			if items, loadErr := client.TorrentCollection(ctx, action.Hash, "files"); loadErr == nil {
				result.apply = func(m *Model) {
					if m.Detail != nil && m.Detail.Torrent.Hash == action.Hash && m.DetailView == DetailFiles {
						m.SetDetailItems(items)
					}
				}
			}
			return result, handled
		}
		return reloadDetail(tr.T("msg.prioritysaved"), "files")
	case ActionSetTrackers:
		if err := client.SetTrackers(ctx, action.Hash, action.Trackers); err != nil {
			action.Domain = "trackers"
			return fail(err)
		}
		return reloadDetail(tr.Format("msg.trackerssaved", len(action.Trackers)), "trackers")
	default:
		return result, false
	}
	return result, true
}
