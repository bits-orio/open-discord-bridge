package main

import (
	"encoding/json"
	"log"
	"os"
	"sort"
	"sync"
)

// linksStore is a thread-safe, file-backed map of player name → linkInfo.
// It is the bridge's source of truth for player↔Discord links across Factorio resets:
// changes are persisted to disk immediately, and on every connection the bridge restores
// the stored links into the mod via /odb-set-link.
type linksStore struct {
	mu   sync.Mutex
	path string
	m    map[string]linkInfo
}

func newLinksStore(path string) *linksStore {
	return &linksStore{path: path, m: make(map[string]linkInfo)}
}

// load reads links from the file into memory. Missing file is treated as empty.
func (ls *linksStore) load() {
	b, err := os.ReadFile(ls.path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("links: read %s: %v", ls.path, err)
		return
	}
	var links []linkInfo
	if err := json.Unmarshal(b, &links); err != nil {
		log.Printf("links: parse %s: %v", ls.path, err)
		return
	}
	ls.mu.Lock()
	for _, l := range links {
		if l.Player != "" && l.DiscordID != "" {
			ls.m[l.Player] = l
		}
	}
	ls.mu.Unlock()
	log.Printf("links: loaded %d link(s) from %s", len(links), ls.path)
}

// save writes the current state to disk atomically. Must be called with ls.mu held.
func (ls *linksStore) save() {
	links := make([]linkInfo, 0, len(ls.m))
	for _, l := range ls.m {
		links = append(links, l)
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Player < links[j].Player })
	b, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		log.Printf("links: marshal: %v", err)
		return
	}
	tmp := ls.path + ".tmp"
	if err := writeFileSynced(tmp, b, 0644); err != nil {
		log.Printf("links: write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, ls.path); err != nil {
		log.Printf("links: rename to %s: %v", ls.path, err)
	}
}

// writeFileSynced writes b to path and fsyncs the file before closing, so the data is on
// disk before the caller renames it into place — otherwise a power loss between write and
// rename can leave the destination empty or truncated after the rename lands.
func writeFileSynced(path string, b []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// upsert adds or replaces a link and saves.
func (ls *linksStore) upsert(l linkInfo) {
	if l.Player == "" || l.DiscordID == "" {
		return
	}
	ls.mu.Lock()
	ls.m[l.Player] = l
	ls.save()
	ls.mu.Unlock()
}

// removeByPlayer removes the link for a Factorio player name and saves.
func (ls *linksStore) removeByPlayer(name string) {
	ls.mu.Lock()
	delete(ls.m, name)
	ls.save()
	ls.mu.Unlock()
}

// all returns a snapshot of all stored links.
func (ls *linksStore) all() []linkInfo {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	out := make([]linkInfo, 0, len(ls.m))
	for _, l := range ls.m {
		out = append(out, l)
	}
	return out
}
