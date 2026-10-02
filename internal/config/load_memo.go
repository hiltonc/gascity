package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/gastownhall/gascity/internal/fsys"
)

// A one-shot gc command composes the same city config many times: context
// resolution, the bd runtime env, every store open and the mail provider each
// load it for themselves, 6 to 30 times per process on a real city
// (bgc-58tk). The load memo lets such a process compose once and hand every
// later caller its own deep copy.
//
// It is off unless the process turns it on with EnableLoadMemo, which cmd/gc
// does only for an allowlist of one-shot commands. A long-lived process never
// enables it: it must see config edits, and nothing here watches for them.
//
// An entry is served only while every file the composition read through its
// filesystem, and the city's .gc/site.toml, still hold the bytes they held,
// so a write to city.toml, a fragment, any pack.toml, an agent.toml or a rig
// binding during the process forces a fresh composition. Not fingerprinted:
// files a composition never read (a directory gaining a new entry) and the
// few reads that bypass the filesystem (os.ReadFile of lock files and system
// packs). The allowlisted commands do not write those, and code that does
// must call InvalidateLoadMemo.

var (
	loadMemoEnabled atomic.Bool
	// loadWithIncludesCalls counts compositions, not memo hits.
	loadWithIncludesCalls atomic.Int64

	loadMemoMu      sync.Mutex
	loadMemoEntries = map[loadMemoKey]*loadMemoEntry{}
)

// loadMemoKey carries every load option that can change what composes.
// SkipRevisionSnapshot and RepoCacheNonBlocking cannot change a successful
// composition, so one entry serves both settings: the snapshot is a prefetch
// that every revision read falls back from (see LoadOptions), and a busy
// repo cache fails a non-blocking composition with ErrRepoCacheBusy rather
// than degrading it, and a failed composition is never stored.
type loadMemoKey struct {
	path                            string
	extraIncludes                   string
	suppressDeprecatedOrderWarnings bool
	allowMissingProviderReferences  bool
	allowLegacyOrderLayouts         bool
}

// loadMemoEntry holds a private copy no caller ever sees, and the bytes each
// fingerprinted file held; a nil value means the file was absent.
type loadMemoEntry struct {
	cfg          *City
	prov         *Provenance
	fingerprints map[string][]byte
}

// EnableLoadMemo turns the load memo on for the rest of this process.
func EnableLoadMemo() { loadMemoEnabled.Store(true) }

// DisableLoadMemo turns the load memo off and drops its entries.
func DisableLoadMemo() {
	loadMemoEnabled.Store(false)
	InvalidateLoadMemo()
}

// LoadMemoEnabled reports whether EnableLoadMemo has run in this process.
func LoadMemoEnabled() bool { return loadMemoEnabled.Load() }

// InvalidateLoadMemo drops every memoized composition. A command that writes
// config the memo does not fingerprint calls it after the write.
func InvalidateLoadMemo() {
	loadMemoMu.Lock()
	defer loadMemoMu.Unlock()
	clear(loadMemoEntries)
}

// LoadWithIncludesCalls reports how many city configs this process has
// composed; memo hits do not count. Tests take a delta across a command.
func LoadWithIncludesCalls() int64 { return loadWithIncludesCalls.Load() }

type composeFunc func(fsys.FS, string, LoadOptions, ...string) (*City, *Provenance, error)

func loadWithIncludesMemo(fs fsys.FS, path string, opts LoadOptions, extraIncludes []string, compose composeFunc) (*City, *Provenance, error) {
	key, memoizable := loadMemoKeyFor(fs, path, opts, extraIncludes)
	if memoizable {
		if cfg, prov, ok := loadMemoLookup(key); ok {
			return cfg, prov, nil
		}
	}
	// Read before composing: a write racing the composition then leaves a
	// fingerprint the next lookup rejects, never a stale one it accepts.
	sitePath := SiteBindingPath(filepath.Dir(path))
	site := readFingerprint(sitePath)
	if memoizable {
		fs = &readRecordingFS{reads: map[string][]byte{}}
	}
	loadWithIncludesCalls.Add(1)
	cfg, prov, err := compose(fs, path, opts, extraIncludes...)
	if err != nil || !memoizable || prov == nil {
		return cfg, prov, err
	}
	// The caller owns what compose returned; the memo keeps its own copy.
	if memoCfg, memoProv, ok := cloneComposition(cfg, prov); ok {
		fingerprints := fs.(*readRecordingFS).snapshot()
		if _, read := fingerprints[sitePath]; !read {
			fingerprints[sitePath] = site
		}
		loadMemoMu.Lock()
		loadMemoEntries[key] = &loadMemoEntry{cfg: memoCfg, prov: memoProv, fingerprints: fingerprints}
		loadMemoMu.Unlock()
	}
	return cfg, prov, nil
}

// loadMemoKeyFor declines the memo for a fake filesystem, which tests swap
// between loads, and for deferred rig patches, which the load writes back
// through a caller's pointer.
func loadMemoKeyFor(fs fsys.FS, path string, opts LoadOptions, extraIncludes []string) (loadMemoKey, bool) {
	if !loadMemoEnabled.Load() {
		return loadMemoKey{}, false
	}
	switch fs.(type) {
	case fsys.OSFS, *fsys.OSFS:
	default:
		return loadMemoKey{}, false
	}
	if opts.deferRigPatches || opts.deferredRigPatches != nil {
		return loadMemoKey{}, false
	}
	return loadMemoKey{
		path:                            path,
		extraIncludes:                   strings.Join(extraIncludes, "\x00"),
		suppressDeprecatedOrderWarnings: opts.SuppressDeprecatedOrderWarnings,
		allowMissingProviderReferences:  opts.AllowMissingProviderReferences,
		allowLegacyOrderLayouts:         opts.allowLegacyOrderLayouts,
	}, true
}

func loadMemoLookup(key loadMemoKey) (*City, *Provenance, bool) {
	loadMemoMu.Lock()
	entry := loadMemoEntries[key]
	loadMemoMu.Unlock()
	if entry == nil {
		return nil, nil, false
	}
	for path, want := range entry.fingerprints {
		got := readFingerprint(path)
		if (got == nil) != (want == nil) || !bytes.Equal(got, want) {
			loadMemoMu.Lock()
			if loadMemoEntries[key] == entry {
				delete(loadMemoEntries, key)
			}
			loadMemoMu.Unlock()
			return nil, nil, false
		}
	}
	// Entries are never mutated after they are stored, so copying needs no lock.
	return cloneComposition(entry.cfg, entry.prov)
}

// readRecordingFS is the OS filesystem, keeping the bytes of every file a
// composition reads so its memo entry can fingerprint each one.
type readRecordingFS struct {
	fsys.OSFS
	mu    sync.Mutex
	reads map[string][]byte
}

func (r *readRecordingFS) ReadFile(name string) ([]byte, error) {
	data, err := r.OSFS.ReadFile(name)
	if err != nil {
		return data, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// The first read wins: if the file changed between two reads, the
	// lookup sees the change and recomposes.
	if _, seen := r.reads[name]; !seen {
		r.reads[name] = append([]byte{}, data...)
	}
	return data, nil
}

func (r *readRecordingFS) snapshot() map[string][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string][]byte, len(r.reads)+1)
	for name, data := range r.reads {
		out[name] = data
	}
	return out
}

// readFingerprint returns a file's bytes, non-nil even when empty, or nil
// when it cannot be read.
func readFingerprint(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if data == nil {
		data = []byte{}
	}
	return data
}

func cloneComposition(cfg *City, prov *Provenance) (*City, *Provenance, bool) {
	cfgCopy, err := deepClone(cfg)
	if err != nil {
		return nil, nil, false
	}
	provCopy, err := deepClone(prov)
	if err != nil {
		return nil, nil, false
	}
	return cfgCopy, provCopy, true
}

// deepClone copies everything reachable from src, unexported fields included,
// so the copy shares no pointer, map or slice backing array with src. Two
// references to one pointer, map or slice in src stay one in the copy. A kind
// whose copy cannot be proved complete (func, chan, interface, unsafe
// pointer) is an error; TestLoadMemoCloneCoversConfigTypeGraph keeps them out
// of City and Provenance.
func deepClone[T any](src *T) (out *T, err error) {
	if src == nil {
		return nil, nil
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("deep clone %T: %v", src, r)
		}
	}()
	c := cloner{seen: map[cloneKey]reflect.Value{}}
	return c.value(reflect.ValueOf(src)).Interface().(*T), nil
}

type cloneKey struct {
	typ      reflect.Type
	ptr      uintptr
	len, cap int
}

type cloner struct {
	seen map[cloneKey]reflect.Value
}

func (c *cloner) value(src reflect.Value) reflect.Value {
	switch src.Kind() {
	case reflect.Ptr:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		key := cloneKey{typ: src.Type(), ptr: src.Pointer()}
		if dst, ok := c.seen[key]; ok {
			return dst
		}
		dst := reflect.New(src.Type().Elem())
		c.seen[key] = dst
		dst.Elem().Set(c.value(src.Elem()))
		return dst
	case reflect.Map:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		key := cloneKey{typ: src.Type(), ptr: src.Pointer()}
		if dst, ok := c.seen[key]; ok {
			return dst
		}
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		c.seen[key] = dst
		for iter := src.MapRange(); iter.Next(); {
			dst.SetMapIndex(c.value(iter.Key()), c.value(iter.Value()))
		}
		return dst
	case reflect.Slice:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		key := cloneKey{typ: src.Type(), ptr: src.Pointer(), len: src.Len(), cap: src.Cap()}
		if dst, ok := c.seen[key]; ok {
			return dst
		}
		full := src.Slice(0, src.Cap())
		dst := reflect.MakeSlice(src.Type(), src.Cap(), src.Cap())
		for i := 0; i < full.Len(); i++ {
			dst.Index(i).Set(c.value(full.Index(i)))
		}
		dst = dst.Slice(0, src.Len())
		c.seen[key] = dst
		return dst
	case reflect.Array:
		dst := reflect.New(src.Type()).Elem()
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(c.value(src.Index(i)))
		}
		return dst
	case reflect.Struct:
		if !src.CanAddr() {
			addressable := reflect.New(src.Type()).Elem()
			addressable.Set(src)
			src = addressable
		}
		dst := reflect.New(src.Type()).Elem()
		for i := 0; i < src.NumField(); i++ {
			exposed(dst.Field(i)).Set(c.value(exposed(src.Field(i))))
		}
		return dst
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src)
		return dst
	default:
		panic(fmt.Sprintf("cannot prove a copy of %s (%s) complete", src.Type(), src.Kind()))
	}
}

// exposed returns an addressable struct field that reflect lets us read and
// set even when it is unexported.
func exposed(field reflect.Value) reflect.Value {
	if field.CanSet() {
		return field
	}
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}
