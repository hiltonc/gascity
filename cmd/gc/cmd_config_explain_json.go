package main

import (
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
)

// configExplainSchemaVersion identifies the document emitted by
// "gc config explain --json" without --provider.
const configExplainSchemaVersion = "1"

// Scopes name what a configExplainEntry configures.
const (
	configExplainScopeWorkspace = "workspace"
	configExplainScopeImport    = "import"
	configExplainScopeRig       = "rig"
	configExplainScopeAgent     = "agent"
	configExplainScopeProvider  = "provider"
)

// Layers name where a configExplainEntry's value was declared.
const (
	configExplainLayerCity     = "city"
	configExplainLayerPack     = "pack"
	configExplainLayerImport   = "import"
	configExplainLayerBuiltin  = "builtin"
	configExplainLayerProvider = "provider"
	configExplainLayerImplicit = "implicit"
)

// configExplainSourceImplicit is the source of a value no file declared,
// matching the sentinel config.Provenance uses for implicit imports.
const configExplainSourceImplicit = "(implicit)"

// configExplainJSON is the "gc config explain --json" document: every
// resolved key of the composed config with its value and provenance.
type configExplainJSON struct {
	SchemaVersion string `json:"schema_version"`
	OK            bool   `json:"ok"`
	CityPath      string `json:"city_path"`
	// Sources lists every config file in load order, root first.
	Sources []string `json:"sources"`
	// Entries holds one row per resolved key, ordered by scope, name, key.
	Entries  []configExplainEntry `json:"entries"`
	Warnings []string             `json:"warnings"`
}

// configExplainEntry is one resolved key.
type configExplainEntry struct {
	// Scope is what the key configures: workspace, import, rig, agent, or
	// provider.
	Scope string `json:"scope"`
	// Name identifies the scoped object: the import binding, rig name,
	// agent qualified name, or provider name. Empty for workspace keys.
	Name string `json:"name,omitempty"`
	// Key is the dotted TOML key within the scoped object, e.g.
	// "provider" or "env.ANTHROPIC_MODEL".
	Key   string `json:"key"`
	Value any    `json:"value"`
	// Layer is where the value was declared: city (city.toml or a city
	// fragment), pack (a pack.toml), import (an import binding),
	// builtin (a built-in provider), provider (a [providers.<name>]
	// table), or implicit (an agent generated for a configured provider).
	Layer string `json:"layer"`
	// Source is the file that declared the value, the provider chain
	// hop ("builtin:<name>" or "providers.<name>") for provider keys, or
	// "(implicit)" for implicit agents and imports.
	Source string `json:"source"`
	// Binding is the import binding an agent arrived through.
	Binding string `json:"binding,omitempty"`
	// Redacted is true when Value is withheld because it is an env value.
	Redacted bool `json:"redacted,omitempty"`
}

// configExplainOptions selects what the explain document covers.
type configExplainOptions struct {
	rigFilter   string
	agentFilter string
	showEnv     bool
}

func doConfigExplainJSON(opts configExplainOptions, stdout, stderr io.Writer) int {
	cityPath, err := resolveCity()
	if err != nil {
		fmt.Fprintf(stderr, "gc config explain: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	if err := ensureLegacyNamedPacksCached(cityPath); err != nil {
		fmt.Fprintf(stderr, "gc config explain: fetching packs: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	cfg, prov, err := loadConfigCommandCityConfig(cityPath)
	if err != nil {
		fmt.Fprintf(stderr, "gc config explain: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	doc, err := buildConfigExplainJSON(cityPath, cfg, prov, opts)
	if err != nil {
		fmt.Fprintf(stderr, "gc config explain: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	return writeCLIJSONLineOrExit(stdout, stderr, "gc config explain", doc)
}

func buildConfigExplainJSON(cityPath string, cfg *config.City, prov *config.Provenance, opts configExplainOptions) (configExplainJSON, error) {
	b := configExplainBuilder{root: prov.Root, showEnv: opts.showEnv}

	if err := b.addFlattened(configForDisplay(cfg).Workspace, configExplainEntry{Scope: configExplainScopeWorkspace}, func(topKey string) string {
		return prov.Workspace[topKey]
	}); err != nil {
		return configExplainJSON{}, fmt.Errorf("workspace: %w", err)
	}

	for _, binding := range slices.Sorted(maps.Keys(cfg.Imports)) {
		source := prov.Imports[binding]
		if source == "" {
			source = prov.Root
		}
		base := configExplainEntry{Scope: configExplainScopeImport, Name: binding, Layer: configExplainLayerImport, Source: source}
		if err := b.addFlattened(cfg.Imports[binding], base, nil); err != nil {
			return configExplainJSON{}, fmt.Errorf("import %q: %w", binding, err)
		}
	}

	for _, rig := range cfg.Rigs {
		source := prov.Rigs[rig.Name]
		if source == "" {
			source = prov.Root
		}
		base := configExplainEntry{Scope: configExplainScopeRig, Name: rig.Name, Layer: b.layerForFile(source), Source: source}
		if err := b.addFlattened(rig, base, nil); err != nil {
			return configExplainJSON{}, fmt.Errorf("rig %q: %w", rig.Name, err)
		}
	}

	matched := 0
	for i := range cfg.Agents {
		a := &cfg.Agents[i]
		if opts.rigFilter != "" && a.Dir != opts.rigFilter {
			continue
		}
		if opts.agentFilter != "" && a.Name != opts.agentFilter {
			continue
		}
		matched++
		qn := a.QualifiedName()
		source := prov.Agents[qn]
		if source == "" {
			source = prov.Root
		}
		base := configExplainEntry{Scope: configExplainScopeAgent, Name: qn, Layer: b.layerForFile(source), Source: source, Binding: a.BindingName}
		switch {
		case a.Implicit:
			base.Layer, base.Source = configExplainLayerImplicit, configExplainSourceImplicit
		case a.BindingName != "":
			base.Layer = configExplainLayerImport
		}
		if err := b.addFlattened(a, base, nil); err != nil {
			return configExplainJSON{}, fmt.Errorf("agent %q: %w", qn, err)
		}
	}
	if matched == 0 && (opts.rigFilter != "" || opts.agentFilter != "") {
		return configExplainJSON{}, fmt.Errorf("no agents match filters (rig=%q agent=%q)", opts.rigFilter, opts.agentFilter)
	}

	for _, name := range slices.Sorted(maps.Keys(cfg.ResolvedProviders)) {
		b.addProvider(name, cfg.ResolvedProviders[name])
	}

	return configExplainJSON{
		SchemaVersion: configExplainSchemaVersion,
		OK:            true,
		CityPath:      cityPath,
		Sources:       nonNilStrings(prov.Sources),
		Entries:       b.entries,
		Warnings:      nonNilStrings(prov.Warnings),
	}, nil
}

type configExplainBuilder struct {
	root    string
	showEnv bool
	entries []configExplainEntry
}

// layerForFile classifies a config file path as a pack or city layer.
func (b *configExplainBuilder) layerForFile(path string) string {
	if path != b.root && filepath.Base(path) == "pack.toml" {
		return configExplainLayerPack
	}
	return configExplainLayerCity
}

// addFlattened appends one entry per leaf TOML key of v. When
// sourceForTopKey is non-nil it supplies a per-top-level-key source
// file, falling back to the provenance root; base.Layer is then derived
// from that file.
func (b *configExplainBuilder) addFlattened(v any, base configExplainEntry, sourceForTopKey func(string) string) error {
	data, err := toml.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding: %w", err)
	}
	var tree map[string]any
	if _, err := toml.Decode(string(data), &tree); err != nil {
		return fmt.Errorf("decoding: %w", err)
	}
	for _, key := range slices.Sorted(maps.Keys(tree)) {
		entry := base
		if sourceForTopKey != nil {
			entry.Source = sourceForTopKey(key)
			if entry.Source == "" {
				entry.Source = b.root
			}
			entry.Layer = b.layerForFile(entry.Source)
		}
		b.addValue(entry, key, key, tree[key], false)
	}
	return nil
}

// addValue appends value under key, descending into tables and arrays
// of tables (keyed by index). name is the last key segment; children of
// a table named "env" are env values.
func (b *configExplainBuilder) addValue(entry configExplainEntry, key, name string, value any, isEnvValue bool) {
	switch v := value.(type) {
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			b.addValue(entry, key+"."+k, k, v[k], name == "env")
		}
		return
	case []map[string]any:
		for i, table := range v {
			index := strconv.Itoa(i)
			b.addValue(entry, key+"."+index, index, table, false)
		}
		return
	}
	entry.Key = key
	entry.Value = value
	if isEnvValue {
		entry.Value, entry.Redacted = b.envValue(name, value)
	}
	b.entries = append(b.entries, entry)
}

// envValue withholds an env value unless --show-env was given or the
// variable names a model (suffix _MODEL).
func (b *configExplainBuilder) envValue(name string, value any) (any, bool) {
	if b.showEnv || strings.HasSuffix(name, "_MODEL") {
		return value, false
	}
	return runtime.RedactedValue, true
}

// addProvider appends every field and map key some chain layer set on a
// resolved custom provider, attributed to that layer.
func (b *configExplainBuilder) addProvider(name string, r config.ResolvedProvider) {
	values := providerResolvedValues(r)
	base := configExplainEntry{Scope: configExplainScopeProvider, Name: name}
	mapFields := map[string]map[string]string{
		"env":              r.Env,
		"permission_modes": r.PermissionModes,
		"option_defaults":  r.EffectiveDefaults,
	}
	for _, field := range slices.Sorted(maps.Keys(r.Provenance.FieldLayer)) {
		if _, isMap := mapFields[field]; isMap {
			continue
		}
		value, ok := values[field]
		if !ok {
			continue
		}
		entry := base
		entry.Key = field
		entry.Value = value
		entry.Source = r.Provenance.FieldLayer[field]
		entry.Layer = providerChainLayer(entry.Source)
		b.entries = append(b.entries, entry)
	}
	for _, field := range slices.Sorted(maps.Keys(r.Provenance.MapKeyLayer)) {
		m, ok := mapFields[field]
		if !ok {
			continue
		}
		layers := r.Provenance.MapKeyLayer[field]
		for _, k := range slices.Sorted(maps.Keys(layers)) {
			value, ok := m[k]
			if !ok {
				continue
			}
			entry := base
			entry.Key = field + "." + k
			entry.Value = value
			if field == "env" {
				entry.Value, entry.Redacted = b.envValue(k, value)
			}
			entry.Source = layers[k]
			entry.Layer = providerChainLayer(entry.Source)
			b.entries = append(b.entries, entry)
		}
	}
}

func providerChainLayer(hop string) string {
	if strings.HasPrefix(hop, config.BasePrefixBuiltin) {
		return configExplainLayerBuiltin
	}
	return configExplainLayerProvider
}
