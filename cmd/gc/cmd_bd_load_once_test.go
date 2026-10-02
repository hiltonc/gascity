package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
)

// TestDoBdLoadsCityConfigOnce pins the passthrough's config budget: one
// composition of city.toml per gc bd, whichever store answers. It was five or
// six (bgc-vpn7): two to name a rig while resolving the city, the command's own
// load, the class routing, the hosted-binding check and the bd binary pin,
// each loading the same file again.
//
// Not parallel: cityConfigLoads and the routing memos are process-wide.
func TestDoBdLoadsCityConfigOnce(t *testing.T) {
	disableManagedDoltRecoveryForTest(t)

	origCityFlag := cityFlag
	origRigFlag := rigFlag
	origProbe := bdBeadExists
	t.Cleanup(func() {
		cityFlag = origCityFlag
		rigFlag = origRigFlag
		bdBeadExists = origProbe
	})
	cityFlag = ""
	rigFlag = ""
	var probed []string
	bdBeadExists = func(_ string, _ *config.City, _ execStoreTarget, beadID string) bool {
		probed = append(probed, beadID)
		return false
	}

	// Initialized scopes, as a working city has: a published managed Dolt
	// for the city and canonical endpoint markers on both stores. A bare
	// .beads directory instead reads as a brand-new scope, whose mode is
	// derived from the desired state with loads of its own.
	cityDir := normalizePathForCompare(t.TempDir())
	rigDir := filepath.Join(cityDir, "repo")
	writeReachableManagedDoltState(t, cityDir)
	for dir, origin := range map[string]string{cityDir: "managed_city", rigDir: "inherited_city"} {
		if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte("gc.endpoint_origin: "+origin+"\ngc.endpoint_status: verified\ndolt.auto-start: false\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(`[workspace]
name = "demo"

[[rigs]]
name = "repo"
path = "repo"
prefix = "repo"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	setCwd(t, cityDir)

	binDir := t.TempDir()
	capture := filepath.Join(t.TempDir(), "gc-bd-scope.txt")
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(`#!/bin/sh
printf '%s\n' "${GC_STORE_SCOPE:-}" > "${CAPTURE_PATH}"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_PATH", capture)
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_CITY_PATH", cityDir)
	t.Setenv("GC_RIG", "")
	t.Setenv("GC_DIR", "")

	for _, tc := range []struct {
		name  string
		args  []string
		scope string
	}{
		{name: "subject routed by its prefix", args: []string{"show", "repo-abc", "--json"}, scope: "rig"},
		{name: "explicit rig", args: []string{"--rig", "repo", "show", "repo-abc", "--json"}, scope: "rig"},
		{name: "city scope", args: []string{"list", "--json"}, scope: "city"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetCLIStorageRoutes(t)
			resetHostedCredentialProbeCache()
			t.Cleanup(resetHostedCredentialProbeCache)

			before := cityConfigLoads.Load()
			var stdout, stderr bytes.Buffer
			if got := doBd(tc.args, &stdout, &stderr); got != 0 {
				t.Fatalf("doBd(%v) = %d, want 0; stderr=%q", tc.args, got, stderr.String())
			}
			if loads := cityConfigLoads.Load() - before; loads != 1 {
				t.Fatalf("gc bd %s composed city.toml %d times, want 1", strings.Join(tc.args, " "), loads)
			}
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(data)); got != tc.scope {
				t.Fatalf("GC_STORE_SCOPE = %q, want %q", got, tc.scope)
			}
		})
	}
	if len(probed) != 0 {
		t.Fatalf("probed the store for %v; a subject whose prefix one scope owns is routed without a probe", probed)
	}
}

// TestResolveBdScopeTargetRoutesSubjectsByOwnedPrefix pins which args route by
// prefix alone. A subject of a by-ID verb whose prefix one scope owns goes
// straight to that scope. Anything that might not be a bead ID still has to be
// found in the store first, and so does a subject whose prefix two scopes
// share.
func TestResolveBdScopeTargetRoutesSubjectsByOwnedPrefix(t *testing.T) {
	setCwd(t, t.TempDir())
	origProbe := bdBeadExists
	t.Cleanup(func() { bdBeadExists = origProbe })
	var probed []string
	bdBeadExists = func(_ string, _ *config.City, target execStoreTarget, beadID string) bool {
		probed = append(probed, target.ScopeKind+":"+target.RigName+":"+beadID)
		return false
	}

	cityDir := filepath.Join(t.TempDir(), "city")
	cfgForTest := func() *config.City {
		return &config.City{
			Workspace: config.Workspace{Name: "gascity"},
			Rigs: []config.Rig{
				{Name: "wren", Path: filepath.Join("rigs", "wren"), Prefix: "wr"},
				{Name: "twin-a", Path: filepath.Join("rigs", "twin-a"), Prefix: "tw"},
				{Name: "twin-b", Path: filepath.Join("rigs", "twin-b"), Prefix: "tw"},
			},
		}
	}
	city := execStoreTarget{ScopeRoot: cityDir, ScopeKind: "city", Prefix: "ga"}
	wren := execStoreTarget{
		ScopeRoot: filepath.Join(cityDir, "rigs", "wren"),
		ScopeKind: "rig",
		Prefix:    "wr",
		RigName:   "wren",
	}

	for _, tc := range []struct {
		name       string
		args       []string
		want       execStoreTarget
		wantProbed []string
	}{
		{name: "show subject", args: []string{"show", "wr-abc", "--json"}, want: wren},
		{name: "subject after a value flag", args: []string{"update", "--status", "open", "wr-abc"}, want: wren},
		{name: "wisp subject", args: []string{"close", "wr-wisp-abc12"}, want: wren},
		{name: "city subject", args: []string{"show", "ga-abc"}, want: city},
		{
			name:       "flag value is not a subject",
			args:       []string{"list", "--label", "wr-abc"},
			want:       city,
			wantProbed: []string{"rig:wren:wr-abc"},
		},
		{
			name:       "create title is not a subject",
			args:       []string{"create", "wr-abc"},
			want:       city,
			wantProbed: []string{"rig:wren:wr-abc"},
		},
		{
			name:       "unknown flag leaves no subjects",
			args:       []string{"show", "--frobnicate", "wr-abc"},
			want:       city,
			wantProbed: []string{"rig:wren:wr-abc"},
		},
		{
			name:       "shared prefix still probes",
			args:       []string{"show", "tw-abc"},
			want:       city,
			wantProbed: []string{"rig:twin-a:tw-abc"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probed = nil
			got, err := resolveBdScopeTarget(cfgForTest(), cityDir, "", tc.args, false, io.Discard)
			if err != nil {
				t.Fatalf("resolveBdScopeTarget() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveBdScopeTarget() = %#v, want %#v", got, tc.want)
			}
			if strings.Join(probed, ",") != strings.Join(tc.wantProbed, ",") {
				t.Fatalf("probed %v, want %v", probed, tc.wantProbed)
			}
		})
	}
}

// TestCLIStorageRoutesTakeAnOfferedUnsplitConfig pins the offer doBd makes: a
// config with no [storage] section answers the routing read without a load of
// its own, and a config that relocates classes is refused, so a split city
// keeps resolving from its own load.
//
// Not parallel: cityConfigLoads and the routing memo are process-wide.
func TestCLIStorageRoutesTakeAnOfferedUnsplitConfig(t *testing.T) {
	cityDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte("[workspace]\nname = \"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resetCLIStorageRoutes(t)
	offerCLIStorageRoutesConfig(cityDir, &config.City{})
	before := cityConfigLoads.Load()
	if routes := cliStorageRoutes(cityDir); routes != nil {
		t.Fatalf("cliStorageRoutes() = %#v, want nil for a city that relocates nothing", routes)
	}
	if loads := cityConfigLoads.Load() - before; loads != 0 {
		t.Fatalf("resolving routes from an offered unsplit config composed city.toml %d times, want 0", loads)
	}

	resetCLIStorageRoutes(t)
	offerCLIStorageRoutesConfig(cityDir, &config.City{Storage: &config.StorageConfig{}})
	before = cityConfigLoads.Load()
	cliStorageRoutes(cityDir)
	if loads := cityConfigLoads.Load() - before; loads != 1 {
		t.Fatalf("an offered config with a [storage] section was used: city.toml composed %d times, want 1", loads)
	}
}

// TestWorkspacePinnedBdBinaryWithConfigReadsTheLoadedConfig pins that the
// one-shot pin lookup answers from the config it is handed, without reading
// city.toml (there is none here), strictness included.
func TestWorkspacePinnedBdBinaryWithConfigReadsTheLoadedConfig(t *testing.T) {
	t.Setenv("BD_BIN", "")
	cityDir := t.TempDir()
	binDir := t.TempDir()
	bd := filepath.Join(binDir, "bd")
	if err := os.WriteFile(bd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	pinned := &config.City{Workspace: config.Workspace{Env: map[string]string{"BD_BIN": bd}}}
	got, err := workspacePinnedBdBinaryWithConfig(cityDir, pinned)
	if err != nil || got != bd {
		t.Fatalf("workspacePinnedBdBinaryWithConfig(BD_BIN pin) = %q, %v; want %q", got, err, bd)
	}

	emptyPath := &config.City{Workspace: config.Workspace{Env: map[string]string{"PATH": t.TempDir()}}}
	if got, err := workspacePinnedBdBinaryWithConfig(cityDir, emptyPath); err == nil {
		t.Fatalf("workspacePinnedBdBinaryWithConfig(PATH without bd) = %q, nil; want the configuration error", got)
	}

	if got, err := workspacePinnedBdBinaryWithConfig(cityDir, &config.City{}); err != nil || got != "" {
		t.Fatalf("workspacePinnedBdBinaryWithConfig(no pin) = %q, %v; want no pin", got, err)
	}
}
