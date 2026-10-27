package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
)

// writeConfigExplainFixtureCity builds a city whose resolved config draws a
// key from each layer: city.toml (workspace, an inline agent, a rig, a
// custom provider, an agent patch), the city's own pack (a convention
// agent), an import binding (an imported pack's agent), and the custom
// provider's implicit agents.
func writeConfigExplainFixtureCity(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rigDir := filepath.Join(dir, "rigs", "myrig")
	writeExplainFixtureFile(t, filepath.Join(rigDir, ".keep"), "")
	writeCityToml(t, dir, `[workspace]
name = "demo"

[[rigs]]
name = "myrig"
path = "`+rigDir+`"

[[rigs.patches]]
agent = "local"

[rigs.patches.env]
RIG_PATCH_TOKEN = "rig-patch-secret"
RIG_PATCH_MODEL = "rig-patch-model"

[[agent]]
name = "local"
provider = "mine"

[agent.env]
ANTHROPIC_API_KEY = "sk-city-secret"
ANTHROPIC_MODEL = "claude-city-model"

[[patches.agent]]
name = "planner"

[patches.agent.env]
CITY_PATCH_TOKEN = "city-patch-secret"
CITY_PATCH_MODEL = "city-patch-model"

[providers.mine]
base = "builtin:claude"

[providers.mine.env]
MINE_TOKEN = "provider-secret"
MINE_MODEL = "provider-model"
`)
	writePackToml(t, dir, `[pack]
name = "demo"
schema = 1

[imports.tools]
source = "./packs/tools"
`)
	writeExplainFixtureFile(t, filepath.Join(dir, "agents", "planner", "prompt.md"), "plan\n")
	writeExplainFixtureFile(t, filepath.Join(dir, "packs", "tools", "pack.toml"), "[pack]\nname = \"tools\"\nschema = 1\n")
	writeExplainFixtureFile(t, filepath.Join(dir, "packs", "tools", "agents", "worker", "prompt.md"), "work\n")
	return dir
}

func writeExplainFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func runConfigExplainJSON(t *testing.T, dir string, extraArgs ...string) (configExplainJSON, []byte) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := append([]string{"--city", dir, "config", "explain", "--json"}, extraArgs...)
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("run(%v) = %d, stderr=%q stdout=%q", args, code, stderr.String(), stdout.String())
	}
	var doc configExplainJSON
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("explain output is not JSON: %v\n%s", err, stdout.String())
	}
	return doc, stdout.Bytes()
}

func findExplainEntry(t *testing.T, doc configExplainJSON, scope, name, key string) configExplainEntry {
	t.Helper()
	for _, e := range doc.Entries {
		if e.Scope == scope && e.Name == name && e.Key == key {
			return e
		}
	}
	t.Fatalf("no entry scope=%q name=%q key=%q in %+v", scope, name, key, doc.Entries)
	return configExplainEntry{}
}

func TestConfigExplainJSONCoversEveryLayerWithSource(t *testing.T) {
	clearGCEnv(t)
	dir := writeConfigExplainFixtureCity(t)
	doc, raw := runConfigExplainJSON(t, dir)

	validateJSONAgainstResultSchema(t, []string{"config", "explain"}, raw)
	assertTopLevelOKTrue(t, raw)

	cityToml := filepath.Join(dir, "city.toml")
	packToml := filepath.Join(dir, "pack.toml")
	toolsPackToml := filepath.Join(dir, "packs", "tools", "pack.toml")

	tests := []struct {
		scope, name, key string
		value            any
		layer, source    string
	}{
		{scope: "workspace", key: "name", value: "demo", layer: "city", source: cityToml},
		{scope: "rig", name: "myrig", key: "name", value: "myrig", layer: "city", source: cityToml},
		{scope: "import", name: "tools", key: "source", value: "./packs/tools", layer: "import", source: packToml},
		{scope: "agent", name: "local", key: "provider", value: "mine", layer: "city", source: cityToml},
		{scope: "agent", name: "planner", key: "name", value: "planner", layer: "pack", source: packToml},
		{scope: "agent", name: "tools.worker", key: "name", value: "worker", layer: "import", source: toolsPackToml},
		{scope: "agent", name: "mine", key: "name", value: "mine", layer: "implicit", source: "(implicit)"},
		{scope: "agent", name: "myrig/mine", key: "dir", value: "myrig", layer: "implicit", source: "(implicit)"},
		{scope: "provider", name: "mine", key: "env.MINE_MODEL", value: "provider-model", layer: "provider", source: "providers.mine"},
	}
	for _, tc := range tests {
		e := findExplainEntry(t, doc, tc.scope, tc.name, tc.key)
		if e.Value != tc.value || e.Layer != tc.layer || e.Source != tc.source {
			t.Errorf("%s %q %s = {value:%v layer:%q source:%q}, want {value:%v layer:%q source:%q}",
				tc.scope, tc.name, tc.key, e.Value, e.Layer, e.Source, tc.value, tc.layer, tc.source)
		}
	}
	if e := findExplainEntry(t, doc, "agent", "tools.worker", "name"); e.Binding != "tools" {
		t.Errorf("imported agent binding = %q, want tools", e.Binding)
	}
	if e := findExplainEntry(t, doc, "provider", "mine", "command"); e.Layer != "builtin" || e.Source != "builtin:claude" {
		t.Errorf("inherited provider command = {layer:%q source:%q}, want {builtin builtin:claude}", e.Layer, e.Source)
	}
	if len(doc.Sources) == 0 || doc.Sources[0] != cityToml {
		t.Errorf("sources = %v, want city.toml first", doc.Sources)
	}
}

func TestConfigExplainJSONRedactsEnvExceptModel(t *testing.T) {
	clearGCEnv(t)
	dir := writeConfigExplainFixtureCity(t)
	doc, raw := runConfigExplainJSON(t, dir)

	for _, secret := range []string{"sk-city-secret", "provider-secret", "rig-patch-secret", "city-patch-secret"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("explain JSON leaks env value %q:\n%s", secret, raw)
		}
	}
	for _, tc := range []struct{ scope, name, key string }{
		{"agent", "local", "env.ANTHROPIC_API_KEY"},
		{"provider", "mine", "env.MINE_TOKEN"},
		{"rig", "myrig", "patches.0.env.RIG_PATCH_TOKEN"},
		{"agent", "planner", "env.CITY_PATCH_TOKEN"},
	} {
		e := findExplainEntry(t, doc, tc.scope, tc.name, tc.key)
		if !e.Redacted || e.Value != runtime.RedactedValue {
			t.Errorf("%s %s = {value:%v redacted:%v}, want redacted", tc.scope, tc.key, e.Value, e.Redacted)
		}
	}
	for _, tc := range []struct{ scope, name, key, want string }{
		{"agent", "local", "env.ANTHROPIC_MODEL", "claude-city-model"},
		{"provider", "mine", "env.MINE_MODEL", "provider-model"},
		{"rig", "myrig", "patches.0.env.RIG_PATCH_MODEL", "rig-patch-model"},
		{"agent", "planner", "env.CITY_PATCH_MODEL", "city-patch-model"},
	} {
		e := findExplainEntry(t, doc, tc.scope, tc.name, tc.key)
		if e.Redacted || e.Value != tc.want {
			t.Errorf("%s %s = {value:%v redacted:%v}, want %q unredacted", tc.scope, tc.key, e.Value, e.Redacted, tc.want)
		}
	}
}

func TestConfigExplainBuilderRedactsEnvAtAnyDepth(t *testing.T) {
	env := map[string]string{"TOKEN": "secret", "X_MODEL": "model"}
	flatten := func(v any) func(*configExplainBuilder) error {
		return func(b *configExplainBuilder) error { return b.addFlattened(v, configExplainEntry{}, nil) }
	}
	tests := []struct {
		name   string
		add    func(*configExplainBuilder) error
		envKey string
	}{
		{"workspace", flatten(config.Workspace{Name: "demo", Env: env}), "env"},
		{"agent", flatten(&config.Agent{Name: "a", Env: env}), "env"},
		{"rig patch", flatten(config.Rig{Name: "r", RigPatches: []config.AgentOverride{{Agent: "a", Env: env}}}), "patches.0.env"},
		{"rig override", flatten(config.Rig{Name: "r", Overrides: []config.AgentOverride{{Agent: "a"}, {Agent: "b", Env: env}}}), "overrides.1.env"},
		{"provider", func(b *configExplainBuilder) error {
			b.addProvider("p", config.ResolvedProvider{Env: env, Provenance: config.ProviderProvenance{
				MapKeyLayer: map[string]map[string]string{"env": {"TOKEN": "providers.p", "X_MODEL": "providers.p"}},
			}})
			return nil
		}, "env"},
	}
	for _, tc := range tests {
		for _, showEnv := range []bool{false, true} {
			b := configExplainBuilder{showEnv: showEnv}
			if err := tc.add(&b); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			got := map[string]configExplainEntry{}
			for _, e := range b.entries {
				got[e.Key] = e
			}
			wantToken, wantRedacted := any(runtime.RedactedValue), true
			if showEnv {
				wantToken, wantRedacted = "secret", false
			}
			if e, ok := got[tc.envKey+".TOKEN"]; !ok || e.Value != wantToken || e.Redacted != wantRedacted {
				t.Errorf("%s showEnv=%v: %s.TOKEN = %+v (found %v), want {value:%v redacted:%v}", tc.name, showEnv, tc.envKey, e, ok, wantToken, wantRedacted)
			}
			if e, ok := got[tc.envKey+".X_MODEL"]; !ok || e.Value != "model" || e.Redacted {
				t.Errorf("%s showEnv=%v: %s.X_MODEL = %+v (found %v), want model unredacted", tc.name, showEnv, tc.envKey, e, ok)
			}
		}
	}
}

func TestConfigExplainJSONShowEnvIncludesSecrets(t *testing.T) {
	clearGCEnv(t)
	dir := writeConfigExplainFixtureCity(t)
	doc, _ := runConfigExplainJSON(t, dir, "--show-env")

	if e := findExplainEntry(t, doc, "agent", "local", "env.ANTHROPIC_API_KEY"); e.Redacted || e.Value != "sk-city-secret" {
		t.Fatalf("--show-env agent env = {value:%v redacted:%v}, want sk-city-secret", e.Value, e.Redacted)
	}
	if e := findExplainEntry(t, doc, "provider", "mine", "env.MINE_TOKEN"); e.Redacted || e.Value != "provider-secret" {
		t.Fatalf("--show-env provider env = {value:%v redacted:%v}, want provider-secret", e.Value, e.Redacted)
	}
}

func TestConfigExplainJSONAgentFilter(t *testing.T) {
	clearGCEnv(t)
	dir := writeConfigExplainFixtureCity(t)
	doc, _ := runConfigExplainJSON(t, dir, "--agent", "planner")

	for _, e := range doc.Entries {
		if e.Scope == "agent" && e.Name != "planner" {
			t.Fatalf("--agent planner emitted agent %q", e.Name)
		}
	}
	findExplainEntry(t, doc, "agent", "planner", "name")
	findExplainEntry(t, doc, "workspace", "", "name")

	doc, _ = runConfigExplainJSON(t, dir, "--rig", "myrig")
	rigAgents := map[string]bool{}
	for _, e := range doc.Entries {
		if e.Scope == "agent" {
			rigAgents[e.Name] = true
		}
	}
	if want := map[string]bool{"myrig/mine": true, "myrig/tools.worker": true}; !maps.Equal(rigAgents, want) {
		t.Fatalf("--rig myrig emitted agents %v, want %v", rigAgents, want)
	}
	findExplainEntry(t, doc, "rig", "myrig", "name")
	findExplainEntry(t, doc, "workspace", "", "name")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--city", dir, "config", "explain", "--json", "--agent", "nobody"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("--agent nobody exit = 0, want nonzero; stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no agents match filters") {
		t.Fatalf("stderr = %q, want no-match diagnostic", stderr.String())
	}
}
