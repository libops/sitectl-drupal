package cmd

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
)

func TestReadCoreExtensionParsesModulesAndThemes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "core.extension.yml")
	data := `_core:
  default_config_hash: abc
module:
  views: 10
  pathauto: 1
  system: 0
theme:
  claro: 0
  olivero: 0
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	files, err := plugin.NewFileAccessor(&config.Context{DockerHostType: config.ContextLocal})
	if err != nil {
		t.Fatalf("NewFileAccessor() error = %v", err)
	}
	defer files.Close()

	modules, themes, err := readCoreExtension(context.Background(), files, path)
	if err != nil {
		t.Fatalf("readCoreExtension() error = %v", err)
	}

	wantModules := []string{"pathauto", "system", "views"}
	if !reflect.DeepEqual(modules, wantModules) {
		t.Fatalf("modules = %v, want %v", modules, wantModules)
	}

	wantThemes := []string{"claro", "olivero"}
	if !reflect.DeepEqual(themes, wantThemes) {
		t.Fatalf("themes = %v, want %v", themes, wantThemes)
	}
}

func TestReadCoreExtensionParsesNonIntegerValues(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "core.extension.yml")
	data := `_core:
  default_config_hash: abc
module:
  pathauto: enabled
  system: 0
  views: true
theme:
  claro: default
  olivero: false
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	files, err := plugin.NewFileAccessor(&config.Context{DockerHostType: config.ContextLocal})
	if err != nil {
		t.Fatalf("NewFileAccessor() error = %v", err)
	}
	defer files.Close()

	modules, themes, err := readCoreExtension(context.Background(), files, path)
	if err != nil {
		t.Fatalf("readCoreExtension() error = %v", err)
	}

	wantModules := []string{"pathauto", "system", "views"}
	if !reflect.DeepEqual(modules, wantModules) {
		t.Fatalf("modules = %v, want %v", modules, wantModules)
	}

	wantThemes := []string{"claro", "olivero"}
	if !reflect.DeepEqual(themes, wantThemes) {
		t.Fatalf("themes = %v, want %v", themes, wantThemes)
	}
}

func TestReadCoreExtensionMissingFileReturnsNilSlices(t *testing.T) {
	files, err := plugin.NewFileAccessor(&config.Context{DockerHostType: config.ContextLocal})
	if err != nil {
		t.Fatalf("NewFileAccessor() error = %v", err)
	}
	defer files.Close()

	modules, themes, err := readCoreExtension(context.Background(), files, filepath.Join(t.TempDir(), "missing.yml"))
	if err != nil {
		t.Fatalf("readCoreExtension() error = %v", err)
	}
	if modules != nil {
		t.Fatalf("expected nil modules, got %v", modules)
	}
	if themes != nil {
		t.Fatalf("expected nil themes, got %v", themes)
	}
}

func TestDrupalRootUsesConfiguredRootfs(t *testing.T) {
	projectDir := t.TempDir()
	drupalRoot := filepath.Join(projectDir, "drupal", "rootfs", "var", "www", "drupal")
	configDir := filepath.Join(drupalRoot, "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "core.extension.yml"), []byte("module: {}\ntheme: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := (&config.Context{ProjectDir: projectDir, DrupalRootfs: "drupal/rootfs/var/www/drupal"}).ResolveProjectPath((&config.Context{ProjectDir: projectDir, DrupalRootfs: "drupal/rootfs/var/www/drupal"}).EffectiveDrupalRootfs())
	if got != drupalRoot {
		t.Fatalf("drupal root = %q, want %q", got, drupalRoot)
	}
}

func TestDrupalRootUsesProjectDirWhenConfigured(t *testing.T) {
	projectDir := t.TempDir()
	ctx := &config.Context{ProjectDir: projectDir, DrupalRootfs: "."}
	got := ctx.ResolveProjectPath(ctx.EffectiveDrupalRootfs())
	if got != projectDir {
		t.Fatalf("drupal root = %q, want %q", got, projectDir)
	}
}

func TestFormatListLinesWrapsThreePerLine(t *testing.T) {
	values := []string{"action", "admin_toolbar", "big_pipe", "views", "pathauto", "token", "media"}

	got := formatListLines(values, 3)
	want := []string{
		"  action, admin_toolbar, big_pipe",
		"  views, pathauto, token",
		"  media",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatListLines() = %v, want %v", got, want)
	}
}

func TestFormatListLinesEmptyReturnsNone(t *testing.T) {
	got := formatListLines(nil, 3)
	want := []string{"  none"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatListLines() = %v, want %v", got, want)
	}
}

func TestParseFirstIntReadsFirstNonEmptyLine(t *testing.T) {
	got, err := parseFirstInt("\n  1024 \nignored\n")
	if err != nil {
		t.Fatalf("parseFirstInt() error = %v", err)
	}
	if got != 1024 {
		t.Fatalf("parseFirstInt() = %d, want 1024", got)
	}
}

func TestParseFirstIntErrorsWhenMissingNumber(t *testing.T) {
	_, err := parseFirstInt("\n \n")
	if err == nil {
		t.Fatal("expected parseFirstInt() error")
	}
	if !strings.Contains(err.Error(), "no numeric output returned") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetDrupalContainerForSDKRequiresSDK(t *testing.T) {
	original := sdk
	sdk = nil
	defer func() { sdk = original }()

	_, _, _, err := getDrupalContainerForSDK(context.Background())
	if err == nil {
		t.Fatal("expected getDrupalContainerForSDK() error")
	}
	if !strings.Contains(err.Error(), "plugin sdk is not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderDrupalDebugRequiresSDK(t *testing.T) {
	original := sdk
	sdk = nil
	defer func() { sdk = original }()

	_, err := renderDrupalDebugBody(context.Background(), &config.Context{}, "")
	if err == nil {
		t.Fatal("expected renderDrupalDebugBody() error")
	}
	if !strings.Contains(err.Error(), "plugin sdk is not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadComposerLockModuleVersionsPrefersDrupalRootAndParsesContribModules(t *testing.T) {
	projectDir := t.TempDir()
	drupalRoot := filepath.Join(projectDir, "web")
	if err := os.MkdirAll(drupalRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	rootLock := `{
		"packages": [
			{"name": "drupal/pathauto", "version": "1.12.0", "type": "drupal-module"},
			{"name": "drupal/token", "version": "1.15.0", "type": "drupal-module"},
			{"name": "drupal/core", "version": "11.1.0", "type": "metapackage"}
		],
		"packages-dev": [
			{"name": "drupal/devel", "version": "5.3.0", "type": "drupal-module"}
		]
	}`
	if err := os.WriteFile(filepath.Join(drupalRoot, "composer.lock"), []byte(rootLock), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	projectLock := `{"packages": [{"name": "drupal/pathauto", "version": "9.9.9", "type": "drupal-module"}]}`
	if err := os.WriteFile(filepath.Join(projectDir, "composer.lock"), []byte(projectLock), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	files, err := plugin.NewFileAccessor(&config.Context{DockerHostType: config.ContextLocal})
	if err != nil {
		t.Fatalf("NewFileAccessor() error = %v", err)
	}
	defer files.Close()

	got, err := readComposerLockModuleVersions(context.Background(), files, drupalRoot, projectDir)
	if err != nil {
		t.Fatalf("readComposerLockModuleVersions() error = %v", err)
	}

	want := composerLockVersionInfo{
		CoreVersion:    "11.1.0",
		ModuleVersions: map[string]string{"devel": "5.3.0", "pathauto": "1.12.0", "token": "1.15.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
}

func TestReadComposerLockModuleVersionsFallsBackToProjectDir(t *testing.T) {
	projectDir := t.TempDir()
	drupalRoot := filepath.Join(projectDir, "web")
	if err := os.MkdirAll(drupalRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	lock := `{"packages": [{"name": "drupal/admin_toolbar", "version": "3.5.0", "type": "drupal-module"}]}`
	if err := os.WriteFile(filepath.Join(projectDir, "composer.lock"), []byte(lock), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	files, err := plugin.NewFileAccessor(&config.Context{DockerHostType: config.ContextLocal})
	if err != nil {
		t.Fatalf("NewFileAccessor() error = %v", err)
	}
	defer files.Close()

	got, err := readComposerLockModuleVersions(context.Background(), files, drupalRoot, projectDir)
	if err != nil {
		t.Fatalf("readComposerLockModuleVersions() error = %v", err)
	}

	want := composerLockVersionInfo{ModuleVersions: map[string]string{"admin_toolbar": "3.5.0"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
}

func TestReadComposerPatchesPrettyPrintsExtraPatches(t *testing.T) {
	projectDir := t.TempDir()
	drupalRoot := filepath.Join(projectDir, "web")
	if err := os.MkdirAll(drupalRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	composerJSON := `{
		"extra": {
			"patches": {
				"drupal/core": {
					"Example patch": "https://example.com/core.patch"
				},
				"drupal/pathauto": {
					"Another patch": "https://example.com/pathauto.patch"
				}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(projectDir, "composer.json"), []byte(composerJSON), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	files, err := plugin.NewFileAccessor(&config.Context{DockerHostType: config.ContextLocal})
	if err != nil {
		t.Fatalf("NewFileAccessor() error = %v", err)
	}
	defer files.Close()

	got, err := readComposerPatches(context.Background(), files, drupalRoot, projectDir)
	if err != nil {
		t.Fatalf("readComposerPatches() error = %v", err)
	}

	want := strings.Join([]string{
		"{",
		"  \"drupal/core\": {",
		"    \"Example patch\": \"https://example.com/core.patch\"",
		"  },",
		"  \"drupal/pathauto\": {",
		"    \"Another patch\": \"https://example.com/pathauto.patch\"",
		"  }",
		"}",
	}, "\n")
	if got != want {
		t.Fatalf("readComposerPatches() = %q, want %q", got, want)
	}
}

func TestRenderModuleListGroupsAndSortsModules(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "core"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "core", "core.services.yml"), []byte("parameters: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	for _, module := range []string{"node", "system"} {
		moduleDir := filepath.Join(root, "web", "core", "modules", module)
		if err := os.MkdirAll(moduleDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(moduleDir, module+".info.yml"), []byte("name: "+module+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	files, err := plugin.NewFileAccessor(&config.Context{DockerHostType: config.ContextLocal})
	if err != nil {
		t.Fatalf("NewFileAccessor() error = %v", err)
	}
	defer files.Close()

	got, err := renderModuleList(context.Background(), files, root, []string{"token", "system", "aaa_custom", "pathauto", "node", "zzz_custom"}, composerLockVersionInfo{CoreVersion: "11.1.0", ModuleVersions: map[string]string{"pathauto": "1.12.0", "token": "1.15.0"}})
	if err != nil {
		t.Fatalf("renderModuleList() error = %v", err)
	}

	want := []string{
		"  Core modules:",
		"  node@11.1.0, system@11.1.0",
		"",
		"  Contrib modules:",
		"  pathauto@1.12.0, token@1.15.0",
		"",
		"  Custom or submodules:",
		"  aaa_custom, zzz_custom",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderModuleList() = %v, want %v", got, want)
	}
}
