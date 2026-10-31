package main

// feed_search.go lets a feed be a Torznab search query instead of an RSS URL
// ("search": "..." in the feeds JSON): the daemon runs the query on every
// configured indexer and hands the merged results to the same rules pipeline
// (dedup, smart-episode, actions) used by RSS feeds.

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/torznab"
)

// searchFeed queries every configured indexer and returns the results as feed
// items. It fails only when nothing came back and at least one indexer errored.
func (d *Daemon) searchFeed(ctx context.Context, name, query string) ([]rss.Item, error) {
	return d.searchFeedLogged(ctx, name, query, true)
}

// searchFeedSuggest is searchFeed for the interactive title suggestions: it does
// not log per-indexer errors, so a down indexer does not flood the log while the
// operator types.
func (d *Daemon) searchFeedSuggest(ctx context.Context, query string) ([]rss.Item, error) {
	return d.searchFeedLogged(ctx, "suggest", query, false)
}

func (d *Daemon) searchFeedLogged(ctx context.Context, name, query string, logErrors bool) ([]rss.Item, error) {
	indexers := d.indexers()
	if len(indexers) == 0 {
		return nil, fmt.Errorf("no indexers configured")
	}
	items, errs := searchIndexers(ctx, indexers, query)
	if logErrors {
		for _, err := range errs {
			logf("feed %s: %v", name, err)
		}
	}
	if len(items) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	return items, nil
}

// searchIndexers runs the query on each indexer and merges the results as feed
// items. Per-indexer errors are returned alongside the items found. The
// indexers are queried concurrently, so a slow or timing-out one does not add
// its latency to the others; the result order still follows the indexer order.
func searchIndexers(ctx context.Context, indexers []torznab.Indexer, query string) ([]rss.Item, []error) {
	query = strings.TrimSpace(query)
	type result struct {
		items []rss.Item
		err   error
	}
	results := make([]result, len(indexers))
	var wg sync.WaitGroup
	for i, idx := range indexers {
		wg.Add(1)
		go func(i int, idx torznab.Indexer) {
			defer wg.Done()
			found, err := idx.Search(ctx, query)
			if err != nil {
				results[i].err = fmt.Errorf("%s: %w", idx.Name, err)
				return
			}
			items := make([]rss.Item, 0, len(found))
			for _, it := range found {
				items = append(items, rssItemFromTorznab(it))
			}
			results[i].items = items
		}(i, idx)
	}
	wg.Wait()

	var items []rss.Item
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		items = append(items, r.items...)
	}
	return items, errs
}

// rssItemFromTorznab maps a search result to the feed item shape. The GUID is
// the download target (magnet or link), which is stable across polls, so the
// "already added" dedup works for search feeds too.
func rssItemFromTorznab(it torznab.Item) rss.Item {
	return rss.Item{
		Title:          it.Title,
		Magnet:         it.Magnet,
		Link:           it.Link,
		Size:           it.Size,
		Seeders:        it.Seeders,
		Leechers:       it.Leechers,
		Published:      it.Published,
		Source:         it.Indexer,
		GUID:           it.Download(),
		Freeleech:      it.Freeleech,
		FreeleechKnown: it.FreeleechKnown,
	}
}
