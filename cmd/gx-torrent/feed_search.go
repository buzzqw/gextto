package main

// feed_search.go lets a feed be a Torznab search query instead of an RSS URL
// ("search": "..." in the feeds JSON): the daemon runs the query on every
// configured indexer and hands the merged results to the same rules pipeline
// (dedup, smart-episode, actions) used by RSS feeds.

import (
	"context"
	"fmt"
	"strings"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/torznab"
)

// searchFeed queries every configured indexer and returns the results as feed
// items. It fails only when nothing came back and at least one indexer errored.
func (d *Daemon) searchFeed(ctx context.Context, name, query string) ([]rss.Item, error) {
	indexers := d.indexers()
	if len(indexers) == 0 {
		return nil, fmt.Errorf("no indexers configured")
	}
	items, errs := searchIndexers(ctx, indexers, query)
	for _, err := range errs {
		logf("feed %s: %v", name, err)
	}
	if len(items) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	return items, nil
}

// searchIndexers runs the query on each indexer and merges the results as feed
// items. Per-indexer errors are returned alongside the items found.
func searchIndexers(ctx context.Context, indexers []torznab.Indexer, query string) ([]rss.Item, []error) {
	query = strings.TrimSpace(query)
	var items []rss.Item
	var errs []error
	for _, idx := range indexers {
		found, err := idx.Search(ctx, query)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", idx.Name, err))
			continue
		}
		for _, it := range found {
			items = append(items, rssItemFromTorznab(it))
		}
	}
	return items, errs
}

// rssItemFromTorznab maps a search result to the feed item shape. The GUID is
// the download target (magnet or link), which is stable across polls, so the
// "already added" dedup works for search feeds too.
func rssItemFromTorznab(it torznab.Item) rss.Item {
	return rss.Item{
		Title:     it.Title,
		Magnet:    it.Magnet,
		Link:      it.Link,
		Size:      it.Size,
		Seeders:   it.Seeders,
		Leechers:  it.Leechers,
		Published: it.Published,
		Source:    it.Indexer,
		GUID:      it.Download(),
	}
}
