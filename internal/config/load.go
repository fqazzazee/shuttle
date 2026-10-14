// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/ignore"
)

// The raw* types mirror the file. Every setting is a pointer so "not set" can
// be told apart from a zero value, which is what lets a layer inherit.

type rawFile struct {
	Global   rawGlobal              `toml:"global"`
	Shares   map[string]rawShare    `toml:"shares"`
	Profiles map[string]rawSettings `toml:"profiles"`
	Shuttles map[string]rawShuttle  `toml:"shuttles"`
}

type rawGlobal struct {
	Bandwidth *rawBandwidth `toml:"bandwidth"`
	Terminal  *string       `toml:"terminal"`
}

type rawShare struct {
	Type   string `toml:"type"`
	Host   string `toml:"host"`
	Port   int    `toml:"port"`
	Share  string `toml:"share"`
	Export string `toml:"export"`
	Path   string `toml:"path"`
	User   string `toml:"user"`
	Domain string `toml:"domain"`
	// nfs3
	MountPort int    `toml:"mount_port"`
	UID       *int64 `toml:"uid"`
	GID       *int64 `toml:"gid"`
	// Decoded only to reject it with a useful message instead of "unknown key".
	Password *string `toml:"password"`
}

type rawSettings struct {
	Mode          *string        `toml:"mode"`
	Triggers      *[]string      `toml:"triggers"`
	PollInterval  *Duration      `toml:"poll_interval"`
	Interval      *Duration      `toml:"interval"`
	At            *[]TimeOfDay   `toml:"at"`
	Conflicts     *string        `toml:"conflicts"`
	ConflictHold  *string        `toml:"conflict_hold"`
	Deletions     *string        `toml:"deletions"`
	DeletionGuard *int           `toml:"deletion_guard"`
	Metered       *string        `toml:"metered"`
	Versioning    *rawVersioning `toml:"versioning"`
	Ignore        *[]string      `toml:"ignore"`
	Bandwidth     *rawBandwidth  `toml:"bandwidth"`
	Notify        *rawNotify     `toml:"notify"`
}

type rawShuttle struct {
	rawSettings
	Profile   string   `toml:"profile"`
	Local     string   `toml:"local"`
	Remote    string   `toml:"remote"`
	Enabled   *bool    `toml:"enabled"`
	IgnoreAdd []string `toml:"ignore_add"`
	Include   []string `toml:"include"`
	Exclude   []string `toml:"exclude"`
}

type rawVersioning struct {
	Kind    string    `toml:"kind"`
	Keep    *Duration `toml:"keep"`
	Count   *int      `toml:"count"`
	SizeCap *ByteSize `toml:"size_cap"`
}

type rawBandwidth struct {
	Up   *Rate `toml:"up"`
	Down *Rate `toml:"down"`
}

type rawNotify struct {
	Errors    *bool `toml:"errors"`
	Conflicts *bool `toml:"conflicts"`
	Completed *bool `toml:"completed"`
	Share     *bool `toml:"share"`
}

// Load reads and validates the config file at path. A leading "~/" in local
// folders is expanded to the user's home directory.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reading the config file the user chose is the point
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(data, home)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes and validates a config document. home replaces a leading "~"
// in local folders. All problems are reported together, joined.
func Parse(data []byte, home string) (*Config, error) {
	var raw rawFile
	dec := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, decodeError(err)
	}

	p := &parser{home: home}
	cfg := &Config{
		Shares:   map[string]Share{},
		Profiles: slices.Sorted(maps.Keys(raw.Profiles)),
	}
	cfg.Bandwidth = layerBandwidth(Bandwidth{}, raw.Global.Bandwidth, nil, SourceDefault)
	if t := raw.Global.Terminal; t != nil {
		cfg.Terminal = strings.Fields(*t)
		if len(cfg.Terminal) == 0 {
			p.errorf("global.terminal: empty; leave it out to use the first terminal found")
		}
	}

	for _, name := range slices.Sorted(maps.Keys(raw.Shares)) {
		// Kept even when invalid (its errors are already recorded), so shuttles
		// using it do not add a misleading "no share named" on top.
		share := raw.Shares[name]
		cfg.Shares[name] = p.share(name, &share)
	}
	for _, name := range cfg.Profiles {
		p.checkName("profiles", name)
		// Resolve each profile on its own too, so a profile no shuttle uses
		// still has its mistakes reported.
		var discard Settings
		prof := raw.Profiles[name]
		p.apply("profiles."+name, &discard, map[string]Source{}, &prof, SourceProfile)
	}
	for _, name := range slices.Sorted(maps.Keys(raw.Shuttles)) {
		r := raw.Shuttles[name]
		if s, ok := p.shuttle(name, &r, raw.Profiles, cfg.Shares); ok {
			cfg.Shuttles = append(cfg.Shuttles, s)
		}
	}
	p.checkOverlaps(cfg)

	if len(p.errs) > 0 {
		return nil, errors.Join(p.errs...)
	}
	return cfg, nil
}

// decodeError turns go-toml's errors into "line N: ..." messages.
func decodeError(err error) error {
	var missing *toml.StrictMissingError
	if errors.As(err, &missing) {
		errs := make([]error, 0, len(missing.Errors))
		for _, e := range missing.Errors {
			row, _ := e.Position()
			errs = append(errs, fmt.Errorf("line %d: unknown key %q", row, strings.Join(e.Key(), ".")))
		}
		return errors.Join(errs...)
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, col := de.Position()
		return fmt.Errorf("line %d, column %d: %s", row, col, de.Error())
	}
	return err
}

type parser struct {
	home string
	errs []error
}

func (p *parser) errorf(format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf(format, args...))
}

// Names become D-Bus arguments and state file names, so they are kept to a
// conservative character set.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (p *parser) checkName(table, name string) {
	if !namePattern.MatchString(name) || strings.Contains(name, "..") {
		p.errorf("%s.%s: name must start with a letter or digit and use only letters, digits, '.', '_' and '-'", table, name)
	}
}

func (p *parser) share(name string, r *rawShare) Share {
	at := "shares." + name
	p.checkName("shares", name)
	if r.Password != nil {
		p.errorf("%s.password: passwords are not stored in the config file; shuttle keeps them in the keyring", at)
	}
	s := Share{Name: name, Type: ShareType(r.Type), Host: r.Host, Port: r.Port, Share: r.Share,
		Export: r.Export, Path: r.Path, User: r.User, Domain: r.Domain, MountPort: r.MountPort}
	need := func(key, val string) {
		if val == "" {
			p.errorf("%s: %s is required for type %q", at, key, r.Type)
		}
	}
	switch s.Type {
	case ShareSMB:
		need("host", r.Host)
		need("share", r.Share)
	case ShareNFS3:
		need("host", r.Host)
		need("export", r.Export)
	case SharePath:
		need("path", r.Path)
		if r.Path != "" {
			s.Path = filepath.Clean(p.expandHome(r.Path))
			if !filepath.IsAbs(s.Path) {
				p.errorf("%s.path: %q must be an absolute path", at, r.Path)
			}
		}
	default:
		p.errorf("%s.type: %q must be one of smb, nfs3, path", at, r.Type)
	}
	for _, port := range []struct {
		key string
		n   int
	}{{"port", r.Port}, {"mount_port", r.MountPort}} {
		if port.n < 0 || port.n > 65535 {
			p.errorf("%s.%s: %d is not a valid port", at, port.key, port.n)
		}
	}
	s.UID = p.id(at+".uid", r.UID)
	s.GID = p.id(at+".gid", r.GID)
	return s
}

// id checks a user or group ID: AUTH_SYS sends it as 32 bits.
func (p *parser) id(at string, v *int64) *uint32 {
	if v == nil {
		return nil
	}
	if *v < 0 || *v > math.MaxUint32 {
		p.errorf("%s: %d is not a valid ID", at, *v)
		return nil
	}
	id := uint32(*v)
	return &id
}

func (p *parser) shuttle(name string, r *rawShuttle, profiles map[string]rawSettings, shares map[string]Share) (Shuttle, bool) {
	at := "shuttles." + name
	before := len(p.errs)
	p.checkName("shuttles", name)

	s := Shuttle{Name: name, Profile: r.Profile, Enabled: true, Settings: Defaults(), Sources: map[string]Source{}}
	if r.Enabled != nil {
		s.Enabled = *r.Enabled
	}
	for _, key := range settingKeys {
		s.Sources[key] = SourceDefault
	}

	// Defaults → profile → shuttle.
	if r.Profile != "" {
		prof, ok := profiles[r.Profile]
		if !ok {
			p.errorf("%s.profile: no profile named %q", at, r.Profile)
		} else {
			// The profile's own mistakes were reported when profiles were checked;
			// a second parser keeps them from repeating once per shuttle.
			quiet := &parser{home: p.home}
			quiet.apply("profiles."+r.Profile, &s.Settings, s.Sources, &prof, SourceProfile)
		}
	}
	p.apply(at, &s.Settings, s.Sources, &r.rawSettings, SourceShuttle)
	if len(r.IgnoreAdd) > 0 {
		s.Settings.Ignore = append(slices.Clone(s.Settings.Ignore), r.IgnoreAdd...)
		s.Sources["ignore_add"] = SourceShuttle
	}
	s.Settings.Include = p.selection(at+".include", r.Include)
	s.Settings.Exclude = p.selection(at+".exclude", r.Exclude)
	if len(r.Include) > 0 && len(r.Exclude) > 0 {
		p.errorf("%s: set include or exclude, not both: a shuttle syncs only what it includes, or all but what it excludes", at)
	}
	// Completion notices default off for real-time shuttles, which finish a
	// run every time a file is saved.
	if s.Sources["notify.completed"] == SourceDefault {
		s.Settings.Notify.Completed = !s.Settings.Has(TriggerRealtime)
	}
	p.checkSettings(at, &s.Settings)

	// Folders.
	if r.Local == "" {
		p.errorf("%s.local: required", at)
	} else {
		s.Local = filepath.Clean(p.expandHome(r.Local))
		if !filepath.IsAbs(s.Local) {
			p.errorf("%s.local: %q must be an absolute path or start with ~/", at, r.Local)
		}
	}
	if r.Remote == "" {
		p.errorf("%s.remote: required, as share:path, e.g. nas:/docs", at)
	} else if shareName, remote, ok := strings.Cut(r.Remote, ":"); !ok {
		p.errorf("%s.remote: %q must be share:path, e.g. nas:/docs", at, r.Remote)
	} else {
		s.Share = shareName
		if _, known := shares[shareName]; !known {
			p.errorf("%s.remote: no share named %q", at, shareName)
		}
		// Accept "nas:", "nas:/", "nas:docs", "nas:/docs/", but never clean
		// away a "..": a path that needs cleaning is rejected, not rewritten.
		s.Remote = strings.TrimSuffix(strings.TrimPrefix(remote, "/"), "/")
		if s.Remote == "" {
			s.Remote = "."
		}
		if !fsys.ValidPath(s.Remote) {
			p.errorf("%s.remote: %q is not a clean path inside the share (no .., //, or ./)", at, remote)
		}
	}
	return s, len(p.errs) == before
}

// settingKeys are the Sources keys every shuttle has.
var settingKeys = []string{
	"mode", "triggers", "poll_interval", "interval", "at", "conflicts", "conflict_hold",
	"deletions", "deletion_guard", "metered", "versioning", "ignore",
	"bandwidth.up", "bandwidth.down",
	"notify.errors", "notify.conflicts", "notify.completed", "notify.share",
}

// apply layers the settings set in r over s, recording src for each one.
func (p *parser) apply(at string, s *Settings, sources map[string]Source, r *rawSettings, src Source) {
	set := func(key string) { sources[key] = src }
	if r.Mode != nil {
		s.Mode = Mode(*r.Mode)
		if !slices.Contains([]Mode{ModePush, ModePull, ModeSync}, s.Mode) {
			p.errorf("%s.mode: %q must be one of push, pull, sync", at, *r.Mode)
		}
		set("mode")
	}
	if r.Triggers != nil {
		s.Triggers = nil
		for _, t := range *r.Triggers {
			switch tr := Trigger(t); tr {
			case TriggerRealtime, TriggerSchedule, TriggerShareAvailable:
				if !slices.Contains(s.Triggers, tr) {
					s.Triggers = append(s.Triggers, tr)
				}
			case "manual": // always available; listing it changes nothing
			default:
				p.errorf("%s.triggers: %q must be one of manual, realtime, schedule, share-available", at, t)
			}
		}
		set("triggers")
	}
	if r.PollInterval != nil {
		s.PollInterval = time.Duration(*r.PollInterval)
		set("poll_interval")
	}
	if r.Interval != nil {
		s.Interval = time.Duration(*r.Interval)
		set("interval")
	}
	if r.At != nil {
		s.At = slices.Clone(*r.At)
		set("at")
	}
	if r.Conflicts != nil {
		s.Conflicts = ConflictPolicy(*r.Conflicts)
		if !slices.Contains([]ConflictPolicy{ConflictKeepBoth, ConflictNewestWins, ConflictAsk}, s.Conflicts) {
			p.errorf("%s.conflicts: %q must be one of keep-both, newest-wins, ask", at, *r.Conflicts)
		}
		set("conflicts")
	}
	if r.ConflictHold != nil {
		s.ConflictHold = HoldScope(*r.ConflictHold)
		if s.ConflictHold != HoldFile && s.ConflictHold != HoldShuttle {
			p.errorf("%s.conflict_hold: %q must be file or shuttle", at, *r.ConflictHold)
		}
		set("conflict_hold")
	}
	if r.Deletions != nil {
		s.Deletions = DeletionMode(*r.Deletions)
		if s.Deletions != DeletionsMirror && s.Deletions != DeletionsAdditive {
			p.errorf("%s.deletions: %q must be mirror or additive", at, *r.Deletions)
		}
		set("deletions")
	}
	if r.DeletionGuard != nil {
		s.DeletionGuard = *r.DeletionGuard
		set("deletion_guard")
	}
	if r.Metered != nil {
		s.Metered = MeteredMode(*r.Metered)
		if s.Metered != MeteredPause && s.Metered != MeteredRun {
			p.errorf("%s.metered: %q must be pause or run", at, *r.Metered)
		}
		set("metered")
	}
	if r.Versioning != nil {
		s.Versioning = p.versioning(at+".versioning", r.Versioning)
		set("versioning")
	}
	if r.Ignore != nil {
		s.Ignore = slices.Clone(*r.Ignore)
		set("ignore")
	}
	s.Bandwidth = layerBandwidth(s.Bandwidth, r.Bandwidth, sources, src)
	if n := r.Notify; n != nil {
		for key, pair := range map[string]struct {
			dst *bool
			val *bool
		}{
			"notify.errors": {&s.Notify.Errors, n.Errors}, "notify.conflicts": {&s.Notify.Conflicts, n.Conflicts},
			"notify.completed": {&s.Notify.Completed, n.Completed}, "notify.share": {&s.Notify.Share, n.Share},
		} {
			if pair.val != nil {
				*pair.dst = *pair.val
				set(key)
			}
		}
	}
}

// layerBandwidth layers the up and down rates of r over b independently.
// sources may be nil when nothing tracks where the values came from.
func layerBandwidth(b Bandwidth, r *rawBandwidth, sources map[string]Source, src Source) Bandwidth {
	if r == nil {
		return b
	}
	if r.Up != nil {
		b.Up = int64(*r.Up)
		if sources != nil {
			sources["bandwidth.up"] = src
		}
	}
	if r.Down != nil {
		b.Down = int64(*r.Down)
		if sources != nil {
			sources["bandwidth.down"] = src
		}
	}
	return b
}

// versioning builds a Versioning from its table, filling per-kind defaults and
// rejecting keys that do not apply to the kind.
// selection checks a shuttle's include or exclude list: paths inside its
// folder, as "photos/2024" or "/photos/2024/".
func (p *parser) selection(at string, paths []string) []string {
	var out []string
	for _, raw := range paths {
		x := strings.TrimSuffix(strings.TrimPrefix(raw, "/"), "/")
		if x == "" || !fsys.ValidPath(x) {
			p.errorf("%s: %q is not a clean path inside the shuttle's folder (no .., //, or ./)", at, raw)
			continue
		}
		out = append(out, x)
	}
	return out
}

func (p *parser) versioning(at string, r *rawVersioning) Versioning {
	v := Versioning{Kind: VersioningKind(r.Kind)}
	if r.SizeCap != nil {
		v.SizeCap = int64(*r.SizeCap)
	}
	only := func(key string, isSet bool, kinds ...VersioningKind) {
		if isSet && !slices.Contains(kinds, v.Kind) {
			p.errorf("%s.%s: does not apply to kind %q", at, key, r.Kind)
		}
	}
	only("keep", r.Keep != nil, VersioningTrash, VersioningStaggered)
	only("count", r.Count != nil, VersioningSimple)
	only("size_cap", r.SizeCap != nil, VersioningTrash, VersioningSimple, VersioningStaggered)

	switch v.Kind {
	case VersioningNone:
	case VersioningTrash:
		v.Keep = 30 * 24 * time.Hour
		if r.Keep != nil {
			v.Keep = time.Duration(*r.Keep)
		}
		if v.Keep <= 0 {
			p.errorf("%s.keep: must be longer than zero for kind \"trash\"", at)
		}
	case VersioningSimple:
		v.Count = 5
		if r.Count != nil {
			v.Count = *r.Count
		}
		if v.Count < 1 {
			p.errorf("%s.count: must be at least 1", at)
		}
	case VersioningStaggered:
		v.Keep = 365 * 24 * time.Hour
		if r.Keep != nil {
			v.Keep = time.Duration(*r.Keep) // 0 keeps versions forever
		}
	default:
		p.errorf("%s.kind: %q must be one of none, trash, simple, staggered", at, r.Kind)
	}
	return v
}

// checkSettings validates the resolved settings as a whole: things no single
// layer can check, like a schedule trigger with no schedule.
func (p *parser) checkSettings(at string, s *Settings) {
	if s.Mode == "" {
		p.errorf("%s: mode is not set; set it in the shuttle or its profile (push, pull or sync)", at)
	}
	if s.Has(TriggerSchedule) && s.Interval == 0 && len(s.At) == 0 {
		p.errorf("%s: trigger \"schedule\" needs interval or at", at)
	}
	if s.PollInterval < time.Second {
		p.errorf("%s.poll_interval: must be at least 1s", at)
	}
	if s.Has(TriggerSchedule) && s.Interval != 0 && s.Interval < time.Minute {
		p.errorf("%s.interval: must be at least 1m", at)
	}
	if _, err := ignore.Compile(s.Ignore); err != nil {
		p.errorf("%s.ignore: %v", at, err)
	}
	if s.DeletionGuard < 0 || s.DeletionGuard > 100 {
		p.errorf("%s.deletion_guard: %d must be a percentage from 0 (off) to 100", at, s.DeletionGuard)
	}
}

// checkOverlaps rejects shuttles whose folders nest inside each other on the
// same side: two shuttles syncing the same files would fight over them.
func (p *parser) checkOverlaps(cfg *Config) {
	type side struct{ shuttle, key, path string }
	var sides []side
	for i := range cfg.Shuttles {
		s := &cfg.Shuttles[i]
		sides = append(sides, side{s.Name, "local:", s.Local})
		share := cfg.Shares[s.Share]
		if share.Type == SharePath {
			// A mounted share is a local path too, and can overlap a local folder.
			sides = append(sides, side{s.Name, "local:", filepath.Join(share.Path, filepath.FromSlash(s.Remote))})
		} else {
			remote := "/" + s.Remote
			if s.Remote == "." {
				remote = "/"
			}
			sides = append(sides, side{s.Name, "share " + s.Share + ":", remote})
		}
	}
	reported := map[[2]string]bool{}
	for i, a := range sides {
		for _, b := range sides[i+1:] {
			if a.key != b.key || !nested(a.path, b.path) {
				continue
			}
			pair := [2]string{a.shuttle, b.shuttle}
			if reported[pair] {
				continue
			}
			reported[pair] = true
			if a.shuttle == b.shuttle {
				p.errorf("shuttles.%s: local and remote folders overlap (%s, %s)", a.shuttle, a.path, b.path)
			} else {
				p.errorf("shuttles.%s and shuttles.%s: folders overlap (%s, %s)", a.shuttle, b.shuttle, a.path, b.path)
			}
		}
	}
}

// nested reports whether a and b are the same path or one contains the other.
func nested(a, b string) bool {
	if a == b {
		return true
	}
	under := func(child, parent string) bool {
		return strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
	}
	return under(a, b) || under(b, a)
}

func (p *parser) expandHome(s string) string {
	if s == "~" {
		return p.home
	}
	if rest, ok := strings.CutPrefix(s, "~/"); ok {
		return filepath.Join(p.home, rest)
	}
	return s
}
