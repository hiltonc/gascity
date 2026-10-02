package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gastownhall/gascity/internal/fsys"
)

// writeLoadMemoCity writes a city whose composition reads a root city.toml, an
// include fragment, the root pack.toml and an imported pack, so a memo entry
// carries several fingerprinted sources and both maps and slices.
func writeLoadMemoCity(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"city.toml": `
include = ["fragment.toml"]

[workspace]
name = "memo"

[[rigs]]
name = "repo"
prefix = "repo"
`,
		".gc/site.toml": `
[[rig]]
name = "repo"
path = "repo"
`,
		"fragment.toml": `
[daemon]
patrol_interval = "1m"
`,
		"pack.toml": `
[pack]
name = "memo"
schema = 2

[imports.gs]
source = "./packs/gastown"

[[agent]]
name = "mayor"
scope = "city"
`,
		"packs/gastown/pack.toml": `
[pack]
name = "gastown"
schema = 2

[[agent]]
name = "dog"
scope = "city"
`,
	}
	for rel, data := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// enableLoadMemoForTest turns the memo on for one test and restores the
// process default (off, empty) afterwards.
func enableLoadMemoForTest(t *testing.T) {
	t.Helper()
	InvalidateLoadMemo()
	EnableLoadMemo()
	t.Cleanup(DisableLoadMemo)
}

func loadMemoTestLoad(t *testing.T, path string, opts LoadOptions) (*City, *Provenance) {
	t.Helper()
	cfg, prov, err := LoadWithIncludesOptions(fsys.OSFS{}, path, opts)
	if err != nil {
		t.Fatalf("LoadWithIncludesOptions: %v", err)
	}
	return cfg, prov
}

// TestLoadMemoCloneCoversConfigTypeGraph is the proof that deepClone's copy is
// complete: every type reachable from City and Provenance is plain data
// (scalars, strings, structs, pointers, slices, arrays, maps). A func, chan,
// interface or unsafe pointer could carry state the clone shares or drops, so
// adding one fails here until deepClone learns to copy it.
func TestLoadMemoCloneCoversConfigTypeGraph(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(ty reflect.Type, path string) {
		if seen[ty] {
			return
		}
		seen[ty] = true
		switch ty.Kind() {
		case reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
			t.Errorf("%s is a %s (%s); deepClone cannot prove its copy complete", path, ty.Kind(), ty)
		case reflect.Ptr, reflect.Slice, reflect.Array:
			walk(ty.Elem(), path+"[]")
		case reflect.Map:
			walk(ty.Key(), path+"{key}")
			walk(ty.Elem(), path+"{value}")
		case reflect.Struct:
			for i := 0; i < ty.NumField(); i++ {
				walk(ty.Field(i).Type, path+"."+ty.Field(i).Name)
			}
		}
	}
	walk(reflect.TypeOf(City{}), "City")
	walk(reflect.TypeOf(Provenance{}), "Provenance")
}

func TestLoadMemoOffByDefaultComposesEveryLoad(t *testing.T) {
	path := filepath.Join(writeLoadMemoCity(t), "city.toml")
	before := LoadWithIncludesCalls()
	loadMemoTestLoad(t, path, LoadOptions{})
	loadMemoTestLoad(t, path, LoadOptions{})
	if got := LoadWithIncludesCalls() - before; got != 2 {
		t.Fatalf("compositions = %d with the memo off, want 2", got)
	}
}

func TestLoadMemoComposesOnceAndServesIndependentCopies(t *testing.T) {
	enableLoadMemoForTest(t)
	path := filepath.Join(writeLoadMemoCity(t), "city.toml")

	before := LoadWithIncludesCalls()
	first, firstProv := loadMemoTestLoad(t, path, LoadOptions{})
	second, secondProv := loadMemoTestLoad(t, path, LoadOptions{})
	third, _ := loadMemoTestLoad(t, path, LoadOptions{})
	if got := LoadWithIncludesCalls() - before; got != 1 {
		t.Fatalf("compositions = %d, want 1", got)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstProv, secondProv) {
		t.Fatal("a memo hit differs from the composition it was copied from")
	}
	if len(first.Agents) == 0 || len(first.Rigs) == 0 || len(firstProv.sourceContents) < 3 {
		t.Fatalf("fixture too thin to prove isolation: %d agents, %d rigs, %d sources",
			len(first.Agents), len(first.Rigs), len(firstProv.sourceContents))
	}

	// Mutate the composition and a hit through every reference kind; the
	// next hit must still match the pristine third copy.
	for _, cfg := range []*City{first, second} {
		cfg.Workspace.Name = "mutated"
		cfg.Agents[0].Name = "mutated"
		cfg.Agents = append(cfg.Agents, Agent{Name: "extra"})
		cfg.Rigs[0].Prefix = "mutated"
	}
	firstProv.Agents["mutated"] = "x"
	secondProv.Sources[0] = "mutated"
	for k := range secondProv.sourceContents {
		secondProv.sourceContents[k][0] ^= 0xff
	}

	fourth, fourthProv := loadMemoTestLoad(t, path, LoadOptions{})
	if !reflect.DeepEqual(third, fourth) {
		t.Fatal("mutating an earlier load leaked into a later memo hit")
	}
	if _, ok := fourthProv.Agents["mutated"]; ok || fourthProv.Sources[0] == "mutated" {
		t.Fatal("mutating an earlier Provenance leaked into a later memo hit")
	}
	if got := LoadWithIncludesCalls() - before; got != 1 {
		t.Fatalf("compositions = %d after mutating copies, want 1 (the memo's own copy must be untouched)", got)
	}
}

func TestLoadMemoRecomposesWhenASourceChanges(t *testing.T) {
	enableLoadMemoForTest(t)
	dir := writeLoadMemoCity(t)
	path := filepath.Join(dir, "city.toml")
	loadMemoTestLoad(t, path, LoadOptions{})

	fragment := filepath.Join(dir, "fragment.toml")
	if err := os.WriteFile(fragment, []byte("[daemon]\npatrol_interval = \"7m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := LoadWithIncludesCalls()
	cfg, _ := loadMemoTestLoad(t, path, LoadOptions{})
	if got := LoadWithIncludesCalls() - before; got != 1 {
		t.Fatalf("compositions after a fragment edit = %d, want 1", got)
	}
	if cfg.Daemon.PatrolInterval != "7m" {
		t.Fatalf("PatrolInterval = %q after the edit, want 7m", cfg.Daemon.PatrolInterval)
	}

	// A rig binding lives in .gc/site.toml, which Provenance does not record.
	site := filepath.Join(dir, ".gc", "site.toml")
	if err := os.WriteFile(site, []byte("[[rig]]\nname = \"repo\"\npath = \"elsewhere\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before = LoadWithIncludesCalls()
	cfg, _ = loadMemoTestLoad(t, path, LoadOptions{})
	if got := LoadWithIncludesCalls() - before; got != 1 {
		t.Fatalf("compositions after a site.toml edit = %d, want 1", got)
	}
	if !strings.HasSuffix(cfg.Rigs[0].Path, "elsewhere") {
		t.Fatalf("Rigs[0].Path = %q after the site.toml edit, want it to end in elsewhere", cfg.Rigs[0].Path)
	}

	// An imported pack's pack.toml is a source too.
	packToml := filepath.Join(dir, "packs", "gastown", "pack.toml")
	if err := os.WriteFile(packToml, []byte("[pack]\nname = \"gastown\"\nschema = 2\n\n[[agent]]\nname = \"cat\"\nscope = \"city\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before = LoadWithIncludesCalls()
	loadMemoTestLoad(t, path, LoadOptions{})
	if got := LoadWithIncludesCalls() - before; got != 1 {
		t.Fatalf("compositions after a pack.toml edit = %d, want 1", got)
	}
}

func TestLoadMemoKeysOnPathAndOptions(t *testing.T) {
	enableLoadMemoForTest(t)
	pathA := filepath.Join(writeLoadMemoCity(t), "city.toml")
	pathB := filepath.Join(writeLoadMemoCity(t), "city.toml")

	before := LoadWithIncludesCalls()
	loadMemoTestLoad(t, pathA, LoadOptions{})
	loadMemoTestLoad(t, pathB, LoadOptions{})
	loadMemoTestLoad(t, pathA, LoadOptions{AllowMissingProviderReferences: true})
	loadMemoTestLoad(t, pathA, LoadOptions{AllowMissingProviderReferences: true})
	loadMemoTestLoad(t, pathA, LoadOptions{SuppressDeprecatedOrderWarnings: true})
	loadMemoTestLoad(t, pathA, LoadOptions{})
	if got := LoadWithIncludesCalls() - before; got != 4 {
		t.Fatalf("compositions = %d, want 4 (two cities, three option sets)", got)
	}
}

// TestLoadMemoSharesAcrossOptionsThatCannotChangeAComposition pins the claim
// loadMemoKey rests on: with the memo off, the revision-snapshot and
// non-blocking settings compose the same City, so one entry may serve them.
func TestLoadMemoSharesAcrossOptionsThatCannotChangeAComposition(t *testing.T) {
	path := filepath.Join(writeLoadMemoCity(t), "city.toml")
	equivalent := []LoadOptions{
		{},
		{SkipRevisionSnapshot: true},
		{RepoCacheNonBlocking: true},
		{SkipRevisionSnapshot: true, RepoCacheNonBlocking: true},
	}
	want, _ := loadMemoTestLoad(t, path, equivalent[0])
	for _, opts := range equivalent[1:] {
		if got, _ := loadMemoTestLoad(t, path, opts); !reflect.DeepEqual(got, want) {
			t.Fatalf("LoadOptions%+v composed a different City; it must not share a memo entry", opts)
		}
	}

	enableLoadMemoForTest(t)
	before := LoadWithIncludesCalls()
	for _, opts := range equivalent {
		loadMemoTestLoad(t, path, opts)
	}
	if got := LoadWithIncludesCalls() - before; got != 1 {
		t.Fatalf("compositions = %d across equivalent options, want 1", got)
	}
}

func TestLoadMemoDeclinesFakeFSAndDeferredRigPatches(t *testing.T) {
	enableLoadMemoForTest(t)
	fake := fsys.NewFake()
	fake.Files["/city/city.toml"] = []byte("[workspace]\nname = \"fake\"\n")
	before := LoadWithIncludesCalls()
	for i := 0; i < 2; i++ {
		if _, _, err := LoadWithIncludesOptions(fake, "/city/city.toml", LoadOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := LoadWithIncludesCalls() - before; got != 2 {
		t.Fatalf("fake-FS compositions = %d, want 2 (never memoized)", got)
	}

	path := filepath.Join(writeLoadMemoCity(t), "city.toml")
	before = LoadWithIncludesCalls()
	for i := 0; i < 2; i++ {
		var deferred []deferredRigPatches
		loadMemoTestLoad(t, path, LoadOptions{deferRigPatches: true, deferredRigPatches: &deferred})
	}
	if got := LoadWithIncludesCalls() - before; got != 2 {
		t.Fatalf("deferred-patch compositions = %d, want 2 (never memoized)", got)
	}
}

// TestDeepCloneSharesNothingAndKeepsAliasing walks a composed config and its
// clone in step. Every pointer, map and non-empty slice in the clone must be
// new memory, and two references to one object in the original must stay one
// object in the clone.
func TestDeepCloneSharesNothingAndKeepsAliasing(t *testing.T) {
	cfg, prov := loadMemoTestLoad(t, filepath.Join(writeLoadMemoCity(t), "city.toml"), LoadOptions{})
	for _, pair := range []struct{ src, dst any }{
		{cfg, mustDeepClone(t, cfg)},
		{prov, mustDeepClone(t, prov)},
	} {
		if !reflect.DeepEqual(pair.src, pair.dst) {
			t.Fatalf("clone of %T differs from its source", pair.src)
		}
		srcToDst := map[uintptr]uintptr{}
		dstToSrc := map[uintptr]uintptr{}
		srcAddrs := map[uintptr]bool{}
		var refs int
		var walk func(src, dst reflect.Value, path string)
		walk = func(src, dst reflect.Value, path string) {
			record := func(s, d uintptr) {
				refs++
				srcAddrs[s] = true
				if prev, ok := srcToDst[s]; ok && prev != d {
					t.Errorf("%s: one source object became two clones", path)
				}
				if prev, ok := dstToSrc[d]; ok && prev != s {
					t.Errorf("%s: two source objects became one clone", path)
				}
				srcToDst[s], dstToSrc[d] = d, s
			}
			switch src.Kind() {
			case reflect.Ptr:
				if src.IsNil() {
					return
				}
				record(src.Pointer(), dst.Pointer())
				walk(src.Elem(), dst.Elem(), path+"*")
			case reflect.Map:
				if src.IsNil() {
					return
				}
				record(src.Pointer(), dst.Pointer())
				for iter := src.MapRange(); iter.Next(); {
					walk(iter.Value(), dst.MapIndex(iter.Key()), path+"{}")
				}
			case reflect.Slice:
				if src.Cap() > 0 {
					record(src.Pointer(), dst.Pointer())
				}
				for i := 0; i < src.Len(); i++ {
					walk(src.Index(i), dst.Index(i), path+"[]")
				}
			case reflect.Array:
				for i := 0; i < src.Len(); i++ {
					walk(src.Index(i), dst.Index(i), path+"[]")
				}
			case reflect.Struct:
				for i := 0; i < src.NumField(); i++ {
					walk(src.Field(i), dst.Field(i), path+"."+src.Type().Field(i).Name)
				}
			}
		}
		walk(reflect.ValueOf(pair.src), reflect.ValueOf(pair.dst), reflect.TypeOf(pair.src).String())
		for d := range dstToSrc {
			if srcAddrs[d] {
				t.Errorf("%T clone shares memory with its source", pair.src)
				break
			}
		}
		if refs < 10 {
			t.Fatalf("%T walk saw %d references; fixture too thin to prove anything", pair.src, refs)
		}
	}
}

func TestDeepCloneRefusesKindsItCannotProve(t *testing.T) {
	type withFunc struct{ F func() }
	if _, err := deepClone(&withFunc{F: func() {}}); err == nil {
		t.Fatal("deepClone copied a func without error")
	}
}

func TestLoadMemoConcurrentLoads(t *testing.T) {
	enableLoadMemoForTest(t)
	path := filepath.Join(writeLoadMemoCity(t), "city.toml")
	want, _ := loadMemoTestLoad(t, path, LoadOptions{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				cfg, _, err := LoadWithIncludesOptions(fsys.OSFS{}, path, LoadOptions{})
				if err != nil {
					t.Error(err)
					return
				}
				if !reflect.DeepEqual(cfg, want) {
					t.Error("concurrent memo hit differs from the composition")
					return
				}
				cfg.Workspace.Name = "mutated"
				cfg.Agents[0].Name = "mutated"
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			InvalidateLoadMemo()
		}
	}()
	wg.Wait()
}

func mustDeepClone[T any](t *testing.T, src *T) *T {
	t.Helper()
	out, err := deepClone(src)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// BenchmarkLoadMemo compares a composition with a memo hit (fingerprint check
// plus deep copy) on the fixture city.
func BenchmarkLoadMemo(b *testing.B) {
	path := filepath.Join(writeLoadMemoCity(b), "city.toml")
	b.Run("compose", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, _, err := loadWithIncludesOptions(fsys.OSFS{}, path, LoadOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("hit", func(b *testing.B) {
		InvalidateLoadMemo()
		EnableLoadMemo()
		defer DisableLoadMemo()
		if _, _, err := LoadWithIncludesOptions(fsys.OSFS{}, path, LoadOptions{}); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := LoadWithIncludesOptions(fsys.OSFS{}, path, LoadOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
