// SPDX-License-Identifier: GPL-2.0-or-later

// Package config loads shuttle's configuration file: shares, profiles and
// shuttles. Each shuttle's effective settings are resolved field by field from
// the built-in defaults, then its profile, then its own overrides, and the
// layer each value came from is recorded so the TUI can show it.
//
// This package must stay free of desktop-specific imports; see the
// portability rule in SPEC.md.
package config

import (
	"time"
)

// Mode is the direction a shuttle moves files in.
type Mode string

const (
	ModePush Mode = "push" // local → share
	ModePull Mode = "pull" // share → local
	ModeSync Mode = "sync" // both ways
)

// Trigger is an automatic reason to run a shuttle. Running on request is
// always possible and is not a Trigger.
type Trigger string

const (
	TriggerRealtime       Trigger = "realtime"
	TriggerSchedule       Trigger = "schedule"
	TriggerShareAvailable Trigger = "share-available"
)

// ConflictPolicy decides what happens when a file changed on both sides of a
// sync shuttle since the last run.
type ConflictPolicy string

const (
	ConflictKeepBoth   ConflictPolicy = "keep-both"
	ConflictNewestWins ConflictPolicy = "newest-wins"
	ConflictAsk        ConflictPolicy = "ask"
)

// HoldScope is what waits while an "ask" conflict is unresolved.
type HoldScope string

const (
	HoldFile    HoldScope = "file"    // only the conflicted file
	HoldShuttle HoldScope = "shuttle" // the whole shuttle
)

// DeletionMode decides whether a push or pull removes files on the
// destination that are gone from the source.
type DeletionMode string

const (
	DeletionsMirror   DeletionMode = "mirror"
	DeletionsAdditive DeletionMode = "additive"
)

// VersioningKind is how replaced and deleted files are kept.
type VersioningKind string

const (
	VersioningNone      VersioningKind = "none"
	VersioningTrash     VersioningKind = "trash"     // keep everything for Keep
	VersioningSimple    VersioningKind = "simple"    // keep the last Count versions
	VersioningStaggered VersioningKind = "staggered" // thin out with age, up to Keep
)

// Versioning is a shuttle's versioning setting. It is overridden as a unit:
// a shuttle that sets versioning replaces its profile's table entirely.
type Versioning struct {
	Kind    VersioningKind
	Keep    time.Duration // trash: how long; staggered: maximum age, 0 for no limit
	Count   int           // simple: versions kept per file
	SizeCap int64         // bytes; 0 means no cap
}

// Bandwidth caps transfer rates in bytes per second; 0 means unlimited.
type Bandwidth struct {
	Up, Down int64
}

// Notify switches desktop notifications per event.
type Notify struct {
	Errors    bool
	Conflicts bool
	Completed bool // a run finished
	Share     bool // the share connected or was lost
}

// Settings are a shuttle's effective, fully resolved settings.
type Settings struct {
	Mode          Mode
	Triggers      []Trigger     // automatic triggers; empty means manual only
	PollInterval  time.Duration // how often the share side is checked for changes
	Interval      time.Duration // schedule trigger: run every Interval
	At            []TimeOfDay   // schedule trigger: run at these local times
	Conflicts     ConflictPolicy
	ConflictHold  HoldScope
	Deletions     DeletionMode
	DeletionGuard int // percent of files gone at once that holds deletions; 0 is off
	Versioning    Versioning
	Ignore        []string
	// Include and Exclude are what a shuttle selects of its folder: slash
	// paths inside it, taken literally (ignore.Matcher.Select). A shuttle
	// sets one or neither; a profile, neither.
	Include, Exclude []string
	Bandwidth        Bandwidth
	Notify           Notify
}

// Has reports whether t is one of the settings' automatic triggers.
func (s *Settings) Has(t Trigger) bool {
	for _, x := range s.Triggers {
		if x == t {
			return true
		}
	}
	return false
}

// Source is the configuration layer an effective setting came from.
type Source uint8

const (
	SourceDefault Source = iota
	SourceProfile
	SourceShuttle
)

func (s Source) String() string {
	switch s {
	case SourceProfile:
		return "profile"
	case SourceShuttle:
		return "shuttle"
	default:
		return "default"
	}
}

// ShareType is how shuttle reaches a share.
type ShareType string

const (
	ShareSMB  ShareType = "smb"
	ShareNFS3 ShareType = "nfs3"
	SharePath ShareType = "path" // an already-mounted directory
)

// Share is a connection to a file server. It never holds a password: those
// live in the Secret Service.
type Share struct {
	Name   string
	Type   ShareType
	Host   string // smb, nfs3
	Port   int    // smb, nfs3; 0 means the protocol default
	Share  string // smb: share name
	Export string // nfs3: export path on the server
	Path   string // path: absolute mount point
	User   string // smb
	Domain string // smb
	// MountPort is where an nfs3 server's mount service listens; 0 asks
	// its portmapper.
	MountPort int
	// UID and GID are who an nfs3 server takes shuttle to be (AUTH_SYS);
	// nil means the user running shuttle.
	UID, GID *uint32
}

// Shuttle is one folder pair with its resolved settings.
type Shuttle struct {
	Name    string
	Profile string // "" when the shuttle uses no profile
	Enabled bool
	Local   string // absolute, cleaned OS path
	Share   string // name of the share in Config.Shares
	Remote  string // slash path inside the share, "." for its root
	// Settings are the effective settings; Sources says where each came from,
	// keyed by config key ("mode", "versioning", "notify.completed", ...).
	Settings Settings
	Sources  map[string]Source
}

// Config is a loaded and validated configuration.
type Config struct {
	Bandwidth Bandwidth // global cap across all shuttles
	// Terminal runs a command in a new terminal window when the command's
	// words are added to it, e.g. ptyxis --new-window --; nil: the first
	// terminal found.
	Terminal []string
	Shares   map[string]Share
	Profiles []string  // profile names, sorted
	Shuttles []Shuttle // sorted by name
}

// Shuttle returns the named shuttle.
func (c *Config) Shuttle(name string) (Shuttle, bool) {
	for i := range c.Shuttles {
		if c.Shuttles[i].Name == name {
			return c.Shuttles[i], true
		}
	}
	return Shuttle{}, false
}

// Defaults are the built-in settings every profile and shuttle starts from.
// Mode has no default: which way files move must be chosen, never assumed.
// The rest lean towards never losing data: manual runs only, additive
// push/pull, and deleted or replaced files kept for 30 days.
func Defaults() Settings {
	return Settings{
		PollInterval:  60 * time.Second,
		Conflicts:     ConflictKeepBoth,
		ConflictHold:  HoldFile,
		Deletions:     DeletionsAdditive,
		DeletionGuard: 50,
		Versioning:    Versioning{Kind: VersioningTrash, Keep: 30 * 24 * time.Hour},
		Notify:        Notify{Errors: true, Conflicts: true, Completed: true, Share: true},
	}
}
