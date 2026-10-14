package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfigExplainFixtureCity builds a city whose resolved config draws a
// key from each layer: city.toml (workspace, an inline agent, a rig, a
// custom provider), the city's own pack (a convention agent), and an
// import binding (an imported pack's agent).
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

[[agent]]
name = "local"
provider = "mine"

[agent.env]
ANTHROPIC_API_KEY = "sk-city-secret"
ANTHROPIC_MODEL = "claude-city-model"

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

	for _, secret := range []string{"sk-city-secret", "provider-secret"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("explain JSON leaks env value %q:\n%s", secret, raw)
		}
	}
	for _, tc := range []struct{ scope, name, key string }{
		{"agent", "local", "env.ANTHROPIC_API_KEY"},
		{"provider", "mine", "env.MINE_TOKEN"},
	} {
		e := findExplainEntry(t, doc, tc.scope, tc.name, tc.key)
		if !e.Redacted || e.Value != configExplainRedactedValue {
			t.Errorf("%s %s = {value:%v redacted:%v}, want redacted", tc.scope, tc.key, e.Value, e.Redacted)
		}
	}
	for _, tc := range []struct{ scope, name, key, want string }{
		{"agent", "local", "env.ANTHROPIC_MODEL", "claude-city-model"},
		{"provider", "mine", "env.MINE_MODEL", "provider-model"},
	} {
		e := findExplainEntry(t, doc, tc.scope, tc.name, tc.key)
		if e.Redacted || e.Value != tc.want {
			t.Errorf("%s %s = {value:%v redacted:%v}, want %q unredacted", tc.scope, tc.key, e.Value, e.Redacted, tc.want)
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

	var stdout, stderr bytes.Buffer
	code := run([]string{"--city", dir, "config", "explain", "--json", "--agent", "nobody"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("--agent nobody exit = 0, want nonzero; stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no agents match filters") {
		t.Fatalf("stderr = %q, want no-match diagnostic", stderr.String())
	}
}
