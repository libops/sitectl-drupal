package cmd

import (
	"strings"
	"testing"

	corecomponent "github.com/libops/sitectl/pkg/component"
)

func TestDrupalMemcachedComponentDefinition(t *testing.T) {
	t.Parallel()

	components, err := drupalServiceComponents()
	if err != nil {
		t.Fatalf("drupalServiceComponents() error = %v", err)
	}
	if len(components) != 3 {
		t.Fatalf("expected three Drupal service components, got %d", len(components))
	}

	definitions := map[string]corecomponent.Definition{}
	for _, component := range components {
		def := component.Definition()
		definitions[def.Name] = def
	}
	for _, name := range []string{"memcached", "ingress", "dev-mode"} {
		if _, ok := definitions[name]; !ok {
			t.Fatalf("expected component %q in definitions %#v", name, definitions)
		}
	}

	def := definitions["memcached"]
	if def.Name != "memcached" {
		t.Fatalf("expected memcached component, got %q", def.Name)
	}
	if def.DefaultState != corecomponent.StateOff || def.DefaultDisposition != corecomponent.DispositionDisabled {
		t.Fatalf("expected disabled default, got state=%q disposition=%q", def.DefaultState, def.DefaultDisposition)
	}
	if allowsDisposition(def, corecomponent.DispositionDistributed) {
		t.Fatalf("AllowedDispositions = %v, did not expect distributed", def.AllowedDispositions)
	}
	if packages := def.ComposerPackagesForEnable(); len(packages) != 1 || packages[0] != memcachedComposerPackage {
		t.Fatalf("expected memcache composer dependency, got %v", packages)
	}

	onRules := def.On.Files.Rules
	assertFileRule(t, onRules, "composer.json", corecomponent.OpSet, ".require."+memcachedComposerPackage, memcachedComposerPackageVersion)
	assertMarkedRule(t, onRules, "Dockerfile", corecomponent.OpSet, memcachedDockerfileStartMarker, "php83-pecl-memcache")
	assertMarkedRule(t, onRules, "assets/default_settings.txt", corecomponent.OpSet, memcachedSettingsStartMarker, "cache.backend.memcache")
	assertMarkedRule(t, def.Off.Files.Rules, "assets/default_settings.txt", corecomponent.OpDelete, memcachedSettingsStartMarker, "")
}

func allowsDisposition(def corecomponent.Definition, want corecomponent.Disposition) bool {
	for _, disposition := range def.AllowedDispositions {
		if disposition == want {
			return true
		}
	}
	return false
}

func assertFileRule(t *testing.T, rules []corecomponent.FileRule, file string, op corecomponent.RuleOp, path string, value any) {
	t.Helper()

	for _, rule := range rules {
		if len(rule.Files) == 1 && rule.Files[0] == file && rule.Op == op && rule.Path == path && rule.Value == value {
			return
		}
	}
	t.Fatalf("expected file rule file=%q op=%q path=%q value=%v, got %#v", file, op, path, value, rules)
}

func assertMarkedRule(t *testing.T, rules []corecomponent.FileRule, file string, op corecomponent.RuleOp, startMarker, content string) {
	t.Helper()

	for _, rule := range rules {
		contentMatches := rule.Content == ""
		if content != "" {
			contentMatches = strings.Contains(rule.Content, content)
		}
		if len(rule.Files) == 1 && rule.Files[0] == file && rule.Op == op && rule.StartMarker == startMarker && contentMatches {
			return
		}
	}
	t.Fatalf("expected marked file rule file=%q op=%q marker=%q content=%q, got %#v", file, op, startMarker, content, rules)
}
