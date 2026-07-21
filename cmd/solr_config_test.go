package cmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
)

func TestSolrConfigRefreshIsRegistered(t *testing.T) {
	commandSDK := plugin.NewSDK(plugin.Metadata{Name: "drupal"})
	if err := RegisterCommands(commandSDK); err != nil {
		t.Fatalf("RegisterCommands() error = %v", err)
	}
	command, _, err := commandSDK.RootCmd.Find([]string{"solr-config", "refresh"})
	if err != nil {
		t.Fatalf("Find(solr-config refresh) error = %v", err)
	}
	if command == nil || command.Name() != "refresh" {
		t.Fatalf("registered command = %#v, want refresh", command)
	}
	for _, name := range []string{"server", "core", "solr-version", "output", "reindex"} {
		if command.Flags().Lookup(name) == nil {
			t.Errorf("refresh command is missing --%s", name)
		}
	}
}

func TestResolveSolrConfigOutputUsesContextLayout(t *testing.T) {
	projectDir := t.TempDir()
	tests := []struct {
		name    string
		context config.Context
		want    string
	}{
		{
			name:    "included ISLE context",
			context: config.Context{Plugin: "isle", ProjectDir: projectDir},
			want:    filepath.Join(projectDir, "drupal/rootfs/opt/solr/server/solr/default/conf"),
		},
		{
			name:    "plain Drupal context",
			context: config.Context{Plugin: "drupal", ProjectDir: projectDir},
			want:    filepath.Join(projectDir, "rootfs/opt/solr/server/solr/default/conf"),
		},
		{
			name: "custom ISLE rootfs",
			context: config.Context{
				Plugin:       "isle",
				ProjectDir:   projectDir,
				DrupalRootfs: "custom/rootfs/var/www/drupal",
			},
			want: filepath.Join(projectDir, "custom/rootfs/opt/solr/server/solr/default/conf"),
		},
		{
			name: "absolute custom ISLE rootfs",
			context: config.Context{
				Plugin:       "isle",
				ProjectDir:   projectDir,
				DrupalRootfs: filepath.Join(projectDir, "custom/rootfs/var/www/drupal"),
			},
			want: filepath.Join(projectDir, "custom/rootfs/opt/solr/server/solr/default/conf"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveSolrConfigOutput(&test.context, "", defaultSolrCore)
			if err != nil {
				t.Fatalf("resolveSolrConfigOutput() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("resolveSolrConfigOutput() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveSolrConfigOutputRejectsProjectEscape(t *testing.T) {
	ctx := &config.Context{Plugin: "drupal", ProjectDir: t.TempDir()}
	_, err := resolveSolrConfigOutput(ctx, "../../outside", defaultSolrCore)
	if err == nil || !strings.Contains(err.Error(), "within context project") {
		t.Fatalf("resolveSolrConfigOutput() error = %v, want project containment error", err)
	}
}

func TestResolveSolrConfigOutputUsesPOSIXPathsForRemoteContext(t *testing.T) {
	ctx := &config.Context{
		Plugin:         "isle",
		DockerHostType: config.ContextRemote,
		ProjectDir:     "/srv/islandora/site",
		DrupalRootfs:   "custom/rootfs/var/www/drupal",
	}
	got, err := resolveSolrConfigOutput(ctx, "", defaultSolrCore)
	if err != nil {
		t.Fatalf("resolveSolrConfigOutput() error = %v", err)
	}
	want := "/srv/islandora/site/custom/rootfs/opt/solr/server/solr/default/conf"
	if got != want {
		t.Fatalf("resolveSolrConfigOutput() = %q, want %q", got, want)
	}
	if _, err := resolveSolrConfigOutput(ctx, "../escape", defaultSolrCore); err == nil {
		t.Fatal("resolveSolrConfigOutput() accepted remote project escape")
	}
}

func TestReadSolrConfigZipAcceptsRegularConfigTree(t *testing.T) {
	archive := makeSolrConfigZip(t, []zipTestEntry{
		{name: "solrconfig.xml", data: []byte("<config/>")},
		{name: "schema.xml", data: []byte("<schema/>")},
		{name: "lang/stopwords_en.txt", data: []byte("a\nan\n")},
	})
	tree, err := readSolrConfigZip(archive)
	if err != nil {
		t.Fatalf("readSolrConfigZip() error = %v", err)
	}
	if got := string(tree["lang/stopwords_en.txt"]); got != "a\nan\n" {
		t.Fatalf("stopwords = %q", got)
	}
}

func TestReadSolrConfigZipRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name  string
		entry zipTestEntry
		match string
	}{
		{name: "traversal", entry: zipTestEntry{name: "../outside", data: []byte("x")}, match: "unsafe archive path"},
		{name: "absolute", entry: zipTestEntry{name: "/outside", data: []byte("x")}, match: "unsafe archive path"},
		{name: "backslash traversal", entry: zipTestEntry{name: `..\outside`, data: []byte("x")}, match: "unsafe archive path"},
		{name: "symlink", entry: zipTestEntry{name: "link", data: []byte("target"), mode: fs.ModeSymlink | 0o777}, match: "symbolic link"},
		{name: "special file", entry: zipTestEntry{name: "pipe", mode: fs.ModeNamedPipe | 0o600}, match: "not a regular file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := []zipTestEntry{
				{name: "solrconfig.xml", data: []byte("<config/>")},
				{name: "schema.xml", data: []byte("<schema/>")},
				test.entry,
			}
			_, err := readSolrConfigZip(makeSolrConfigZip(t, entries))
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("readSolrConfigZip() error = %v, want %q", err, test.match)
			}
		})
	}
}

func TestReadSolrConfigZipRejectsOversizedFile(t *testing.T) {
	archive := makeSolrConfigZip(t, []zipTestEntry{
		{name: "solrconfig.xml", data: []byte("<config/>")},
		{name: "schema.xml", data: []byte("<schema/>")},
		{name: "huge.txt", data: bytes.Repeat([]byte("x"), int(maxSolrConfigFileBytes)+1)},
	})
	_, err := readSolrConfigZip(archive)
	if err == nil || !strings.Contains(err.Error(), "size limits") {
		t.Fatalf("readSolrConfigZip() error = %v, want size limit error", err)
	}
}

func TestReadSingleRegularFileTarRejectsSymlinkAndTraversal(t *testing.T) {
	tests := []struct {
		name   string
		header tar.Header
	}{
		{name: "symlink", header: tar.Header{Name: "config.zip", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		{name: "traversal", header: tar.Header{Name: "../config.zip", Typeflag: tar.TypeReg, Size: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var archive bytes.Buffer
			writer := tar.NewWriter(&archive)
			if err := writer.WriteHeader(&test.header); err != nil {
				t.Fatal(err)
			}
			if test.header.Size > 0 {
				if _, err := writer.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := readSingleRegularFileTar(&archive, "config.zip", 1024); err == nil {
				t.Fatal("readSingleRegularFileTar() unexpectedly accepted unsafe archive")
			}
		})
	}
}

func TestRefreshSolrConfigCurrentTreesAreNoOp(t *testing.T) {
	generated := testSolrConfigTree()
	runtime := newFakeSolrConfigRuntime(t, generated, generated)
	host := &fakeSolrConfigHostStore{tree: cloneSolrConfigTree(generated), exists: true}

	result, err := refreshSolrConfig(context.Background(), solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  "site-drupal-1",
		SolrContainer:    "site-solr-1",
		DrupalWorkingDir: "/var/www/drupal",
		OutputPath:       "/project/rootfs/opt/solr/server/solr/default/conf",
	}, solrConfigOptions{Server: defaultSolrConfigServer, Core: defaultSolrCore, SolrVersion: "9.10.1", Reindex: true})
	if err != nil {
		t.Fatalf("refreshSolrConfig() error = %v", err)
	}
	if result.HostUpdated || result.RuntimeUpdated || result.Reindexed {
		t.Fatalf("refresh result = %+v, want no changes", result)
	}
	if host.publishCalls != 0 || runtime.copyToCalls != 0 {
		t.Fatalf("mutations: host publishes=%d runtime copies=%d", host.publishCalls, runtime.copyToCalls)
	}
	for _, call := range runtime.execCalls {
		if len(call) == 0 {
			continue
		}
		if call[0] == "mkdir" || call[0] == "mv" || call[0] == "curl" || strings.HasPrefix(call[0], "search-api:") {
			t.Fatalf("unexpected no-op mutation/reload call: %v", call)
		}
		if call[0] == "bash" || containsString(call, "-lc") {
			t.Fatalf("container shell invocation is not allowed: %v", call)
		}
	}
	if !runtime.sawDrushConfigGeneration() {
		t.Fatal("expected native Drush config generation call")
	}
}

func TestRefreshSolrConfigRuntimeDriftReloadsAndReindexes(t *testing.T) {
	generated := testSolrConfigTree()
	runtimeTree := cloneSolrConfigTree(generated)
	runtimeTree["schema.xml"] = []byte("old")
	runtime := newFakeSolrConfigRuntime(t, generated, runtimeTree)
	runtime.coreExists = true
	host := &fakeSolrConfigHostStore{tree: cloneSolrConfigTree(generated), exists: true}

	result, err := refreshSolrConfig(context.Background(), solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  "site-drupal-1",
		SolrContainer:    "site-solr-1",
		DrupalWorkingDir: "/var/www/drupal",
		OutputPath:       "/project/drupal/rootfs/opt/solr/server/solr/default/conf",
	}, solrConfigOptions{Server: defaultSolrConfigServer, Core: defaultSolrCore, SolrVersion: "9.10.1", Reindex: true})
	if err != nil {
		t.Fatalf("refreshSolrConfig() error = %v", err)
	}
	if result.HostUpdated || !result.RuntimeUpdated || result.CoreAction != "reloaded" || !result.Reindexed {
		t.Fatalf("refresh result = %+v", result)
	}
	if runtime.stagedConfigCopies != 1 {
		t.Fatalf("staged config copies = %d, want 1", runtime.stagedConfigCopies)
	}
	if !runtime.sawSolrAction("STATUS") || !runtime.sawSolrAction("RELOAD") {
		t.Fatalf("Solr API calls = %v, want STATUS and RELOAD", runtime.execCalls)
	}
	if !runtime.sawExec([]string{"drush", "-y", "search-api:reset-tracker"}) || !runtime.sawExec([]string{"drush", "-y", "search-api:index"}) {
		t.Fatalf("Drush calls = %v, want reset and index", runtime.execCalls)
	}
	for _, call := range runtime.execCalls {
		if containsString(call, path.Join(solrContainerRoot, defaultSolrCore, "data")) {
			t.Fatalf("runtime replacement touched data: %v", call)
		}
		if call[0] == "bash" || containsString(call, "-lc") {
			t.Fatalf("container shell invocation is not allowed: %v", call)
		}
	}
}

func TestRefreshSolrConfigHostOnlyDriftDoesNotReloadOrReindex(t *testing.T) {
	generated := testSolrConfigTree()
	runtime := newFakeSolrConfigRuntime(t, generated, generated)
	host := &fakeSolrConfigHostStore{exists: false}

	result, err := refreshSolrConfig(context.Background(), solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  "site-drupal-1",
		SolrContainer:    "site-solr-1",
		DrupalWorkingDir: "/var/www/drupal",
		OutputPath:       "/project/rootfs/opt/solr/server/solr/default/conf",
	}, solrConfigOptions{Server: defaultSolrConfigServer, Core: defaultSolrCore, SolrVersion: "9.10.1", Reindex: true})
	if err != nil {
		t.Fatalf("refreshSolrConfig() error = %v", err)
	}
	if !result.HostUpdated || result.RuntimeUpdated || result.Reindexed {
		t.Fatalf("refresh result = %+v", result)
	}
	if host.publishCalls != 1 || runtime.copyToCalls != 0 || runtime.sawSolrAction("STATUS") {
		t.Fatalf("host publishes=%d runtime copies=%d calls=%v", host.publishCalls, runtime.copyToCalls, runtime.execCalls)
	}
}

func TestRefreshSolrConfigSkipsWhenSearchAPISolrIsDisabled(t *testing.T) {
	generated := testSolrConfigTree()
	runtime := newFakeSolrConfigRuntime(t, generated, generated)
	runtime.moduleEnabled = false
	host := &fakeSolrConfigHostStore{tree: cloneSolrConfigTree(generated), exists: true}

	result, err := refreshSolrConfig(context.Background(), solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  "site-drupal-1",
		SolrContainer:    "site-solr-1",
		DrupalWorkingDir: "/var/www/drupal",
		OutputPath:       "/project/rootfs/opt/solr/server/solr/default/conf",
	}, solrConfigOptions{Server: defaultSolrConfigServer, Core: defaultSolrCore, SolrVersion: "9.10.1"})
	if err != nil {
		t.Fatalf("refreshSolrConfig() error = %v", err)
	}
	if !result.Skipped || !strings.Contains(result.SkipReason, "not enabled") {
		t.Fatalf("refresh result = %+v, want explicit disabled-module skip", result)
	}
	if runtime.sawDrushConfigGeneration() || runtime.copyToCalls != 0 || host.publishCalls != 0 {
		t.Fatalf("disabled-module skip mutated state: calls=%v copies=%d publishes=%d", runtime.execCalls, runtime.copyToCalls, host.publishCalls)
	}
}

func TestRefreshSolrConfigRetriesReloadAndReindexFromDurableState(t *testing.T) {
	generated := testSolrConfigTree()
	previous := cloneSolrConfigTree(generated)
	previous["schema.xml"] = []byte("old")
	runtime := newFakeSolrConfigRuntime(t, generated, previous)
	runtime.coreExists = true
	runtime.failSolrActions["RELOAD"] = 1
	runtime.failDrush["search-api:index"] = 1
	host := &fakeSolrConfigHostStore{tree: cloneSolrConfigTree(generated), exists: true}
	dependencies := solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  "site-drupal-1",
		SolrContainer:    "site-solr-1",
		DrupalWorkingDir: "/var/www/drupal",
		OutputPath:       "/project/rootfs/opt/solr/server/solr/default/conf",
	}
	options := solrConfigOptions{Server: defaultSolrConfigServer, Core: defaultSolrCore, SolrVersion: "9.10.1", Reindex: true}
	paths := solrPendingPaths(defaultSolrCore)

	if _, err := refreshSolrConfig(context.Background(), dependencies, options); err == nil || !strings.Contains(err.Error(), "RELOAD") {
		t.Fatalf("first refresh error = %v, want injected reload failure", err)
	}
	if got := runtime.trees[paths.Conf]; !solrConfigTreesEqual(got, previous) {
		t.Fatalf("conf after rejected reload = %#v, want previous tree", got)
	}
	if _, ok := runtime.files[paths.State]; !ok {
		t.Fatal("reload failure did not retain durable pending state")
	}

	if _, err := refreshSolrConfig(context.Background(), dependencies, options); err == nil || !strings.Contains(err.Error(), "search-api:index") {
		t.Fatalf("second refresh error = %v, want injected reindex failure", err)
	}
	if got := runtime.trees[paths.Conf]; !solrConfigTreesEqual(got, generated) {
		t.Fatalf("conf after accepted retry = %#v, want generated tree", got)
	}
	if _, ok := runtime.files[paths.State]; !ok {
		t.Fatal("reindex failure did not retain durable pending state")
	}
	if _, ok := runtime.trees[paths.Previous]; ok {
		t.Fatal("previous conf remained after Solr accepted replacement")
	}

	result, err := refreshSolrConfig(context.Background(), dependencies, options)
	if err != nil {
		t.Fatalf("third refresh error = %v", err)
	}
	if !result.RuntimeUpdated || !result.Reindexed || result.CoreAction != "reloaded" {
		t.Fatalf("third refresh result = %+v, want resumed reindex completion", result)
	}
	if runtime.sawSolrActionCount("RELOAD") != 3 {
		t.Fatalf("RELOAD calls = %d, want rejected replacement, restored config, and accepted retry", runtime.sawSolrActionCount("RELOAD"))
	}
	if runtime.sawExecCount([]string{"drush", "-y", "search-api:reset-tracker"}) != 1 {
		t.Fatalf("search-api:reset-tracker calls = %d, want one durable reset", runtime.sawExecCount([]string{"drush", "-y", "search-api:reset-tracker"}))
	}
	if runtime.sawExecCount([]string{"drush", "-y", "search-api:index"}) != 2 {
		t.Fatalf("search-api:index calls = %d, want 2", runtime.sawExecCount([]string{"drush", "-y", "search-api:index"}))
	}
	if runtime.directories[paths.Pending] || runtime.files[paths.State] != nil {
		t.Fatal("completed retry left a pending marker")
	}
}

func TestRefreshSolrConfigReconcilesStaleAcceptedTransaction(t *testing.T) {
	generated := testSolrConfigTree()
	current := cloneSolrConfigTree(generated)
	current["schema.xml"] = []byte("externally changed")
	runtime := newFakeSolrConfigRuntime(t, generated, current)
	runtime.coreExists = true
	host := &fakeSolrConfigHostStore{tree: cloneSolrConfigTree(generated), exists: true}
	dependencies := solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  "site-drupal-1",
		SolrContainer:    "site-solr-1",
		DrupalWorkingDir: "/var/www/drupal",
		OutputPath:       "/project/rootfs/opt/solr/server/solr/default/conf",
	}
	paths := solrPendingPaths(defaultSolrCore)
	runtime.addFakeDirectory(paths.Pending)
	state := pendingSolrConfigState{
		Version:        pendingSolrConfigVersion,
		DesiredSHA256:  solrConfigTreeSHA256(generated),
		Reindex:        true,
		CoreReconciled: true,
		CoreAction:     "reloaded",
	}
	if err := writePendingSolrConfigState(context.Background(), runtime, dependencies.SolrContainer, paths, state); err != nil {
		t.Fatal(err)
	}

	result, err := refreshSolrConfig(context.Background(), dependencies, solrConfigOptions{
		Server:      defaultSolrConfigServer,
		Core:        defaultSolrCore,
		SolrVersion: "9.10.1",
	})
	if err != nil {
		t.Fatalf("refreshSolrConfig() error = %v", err)
	}
	if !result.RuntimeUpdated || !result.Reindexed || result.CoreAction != "reloaded" {
		t.Fatalf("refresh result = %+v, want forced reconciliation with retained reindex intent", result)
	}
	if got := runtime.trees[paths.Conf]; !solrConfigTreesEqual(got, generated) {
		t.Fatalf("reconciled conf = %#v, want generated tree", got)
	}
	if runtime.directories[paths.Pending] || runtime.files[paths.State] != nil {
		t.Fatal("reconciled stale transaction left a pending marker")
	}
}

func TestRefreshSolrConfigSupersedesAcceptedPendingDigestBeforeReindex(t *testing.T) {
	generated := testSolrConfigTree()
	accepted := cloneSolrConfigTree(generated)
	accepted["schema.xml"] = []byte("accepted before module update")
	runtime := newFakeSolrConfigRuntime(t, generated, accepted)
	runtime.coreExists = true
	host := &fakeSolrConfigHostStore{tree: cloneSolrConfigTree(generated), exists: true}
	dependencies := solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  "site-drupal-1",
		SolrContainer:    "site-solr-1",
		DrupalWorkingDir: "/var/www/drupal",
		OutputPath:       "/project/rootfs/opt/solr/server/solr/default/conf",
	}
	paths := solrPendingPaths(defaultSolrCore)
	runtime.addFakeDirectory(paths.Pending)
	state := pendingSolrConfigState{
		Version:        pendingSolrConfigVersion,
		DesiredSHA256:  solrConfigTreeSHA256(accepted),
		Reindex:        true,
		CoreReconciled: true,
		CoreAction:     "reloaded",
	}
	if err := writePendingSolrConfigState(context.Background(), runtime, dependencies.SolrContainer, paths, state); err != nil {
		t.Fatal(err)
	}

	result, err := refreshSolrConfig(context.Background(), dependencies, solrConfigOptions{
		Server:      defaultSolrConfigServer,
		Core:        defaultSolrCore,
		SolrVersion: "9.10.1",
	})
	if err != nil {
		t.Fatalf("refreshSolrConfig() error = %v", err)
	}
	if !result.RuntimeUpdated || !result.Reindexed || result.CoreAction != "reloaded" {
		t.Fatalf("refresh result = %+v, want superseding config and retained reindex", result)
	}
	if got := runtime.trees[paths.Conf]; !solrConfigTreesEqual(got, generated) {
		t.Fatalf("superseding conf = %#v, want newly generated tree", got)
	}
	for _, command := range []string{"search-api:reset-tracker", "search-api:index"} {
		if count := runtime.sawExecCount([]string{"drush", "-y", command}); count != 1 {
			t.Fatalf("%s calls = %d, want reindex only after superseding config", command, count)
		}
	}
}

func TestRefreshSolrConfigDoesNotRepeatCompletedReindexAfterCleanupFailure(t *testing.T) {
	generated := testSolrConfigTree()
	current := cloneSolrConfigTree(generated)
	current["schema.xml"] = []byte("old")
	runtime := newFakeSolrConfigRuntime(t, generated, current)
	runtime.coreExists = true
	host := &fakeSolrConfigHostStore{tree: cloneSolrConfigTree(generated), exists: true}
	dependencies := solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  "site-drupal-1",
		SolrContainer:    "site-solr-1",
		DrupalWorkingDir: "/var/www/drupal",
		OutputPath:       "/project/rootfs/opt/solr/server/solr/default/conf",
	}
	options := solrConfigOptions{Server: defaultSolrConfigServer, Core: defaultSolrCore, SolrVersion: "9.10.1", Reindex: true}
	paths := solrPendingPaths(defaultSolrCore)
	runtime.failRemove[paths.Pending] = 1

	if _, err := refreshSolrConfig(context.Background(), dependencies, options); err == nil || !strings.Contains(err.Error(), "remove completed pending") {
		t.Fatalf("first refresh error = %v, want injected pending cleanup failure", err)
	}
	result, err := refreshSolrConfig(context.Background(), dependencies, options)
	if err != nil {
		t.Fatalf("second refresh error = %v", err)
	}
	if !result.RuntimeUpdated || !result.Reindexed {
		t.Fatalf("second refresh result = %+v, want completed transaction cleanup", result)
	}
	for _, command := range []string{"search-api:reset-tracker", "search-api:index"} {
		if count := runtime.sawExecCount([]string{"drush", "-y", command}); count != 1 {
			t.Fatalf("%s calls = %d, want completed phase not to repeat", command, count)
		}
	}
}

func TestDetectSolrVersionUsesRunningSystemAPI(t *testing.T) {
	tree := testSolrConfigTree()
	runtime := newFakeSolrConfigRuntime(t, tree, tree)
	version, err := detectSolrVersion(context.Background(), runtime, "site-solr-1")
	if err != nil {
		t.Fatalf("detectSolrVersion() error = %v", err)
	}
	if version != "10.0.0" {
		t.Fatalf("detectSolrVersion() = %q, want 10.0.0", version)
	}
}

func TestReconcileSolrCoreCreatesMissingCore(t *testing.T) {
	tree := testSolrConfigTree()
	runtime := newFakeSolrConfigRuntime(t, tree, tree)
	action, err := reconcileSolrCore(context.Background(), runtime, "site-solr-1", defaultSolrCore)
	if err != nil {
		t.Fatalf("reconcileSolrCore() error = %v", err)
	}
	if action != "created" || !runtime.sawSolrAction("CREATE") {
		t.Fatalf("action = %q, calls = %v; want CREATE", action, runtime.execCalls)
	}
}

func TestLocalSolrConfigHostStorePublishesWholeTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "conf")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "obsolete.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree := testSolrConfigTree()
	store := localSolrConfigHostStore{projectRoot: filepath.Dir(root)}
	if err := store.PublishTree(context.Background(), root, tree); err != nil {
		t.Fatalf("PublishTree() error = %v", err)
	}
	got, err := store.ReadTree(context.Background(), root)
	if err != nil {
		t.Fatalf("ReadTree() error = %v", err)
	}
	if !solrConfigTreesEqual(got, tree) {
		t.Fatalf("published tree = %#v, want %#v", got, tree)
	}
	if _, err := os.Stat(filepath.Join(root, "obsolete.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("obsolete file still exists or stat failed: %v", err)
	}
	matches, err := filepath.Glob(root + ".sitectl-backup-*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("backup leftovers = %v, error = %v", matches, err)
	}
}

func TestLocalSolrConfigHostStoreRejectsSymlinkedParentEscape(t *testing.T) {
	projectRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(projectRoot, "solr-link")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectRoot, "solr-link", "conf")
	store := localSolrConfigHostStore{projectRoot: projectRoot}
	err := store.PublishTree(context.Background(), target, testSolrConfigTree())
	if err == nil || !strings.Contains(err.Error(), "outside context project") {
		t.Fatalf("PublishTree() error = %v, want symlink containment error", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "conf")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("symlink escape created outside target: %v", err)
	}
}

type zipTestEntry struct {
	name string
	data []byte
	mode fs.FileMode
}

func makeSolrConfigZip(t *testing.T, entries []zipTestEntry) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func testSolrConfigTree() solrConfigTree {
	return solrConfigTree{
		"lang/stopwords_en.txt": []byte("a\nan\n"),
		"schema.xml":            []byte("<schema/>"),
		"solrconfig.xml":        []byte("<config/>"),
	}
}

func cloneSolrConfigTree(tree solrConfigTree) solrConfigTree {
	clone := make(solrConfigTree, len(tree))
	for name, data := range tree {
		clone[name] = bytes.Clone(data)
	}
	return clone
}

type fakeSolrConfigHostStore struct {
	tree         solrConfigTree
	exists       bool
	publishCalls int
}

func (s *fakeSolrConfigHostStore) ReadTree(context.Context, string) (solrConfigTree, error) {
	if !s.exists {
		return nil, fs.ErrNotExist
	}
	return cloneSolrConfigTree(s.tree), nil
}

func (s *fakeSolrConfigHostStore) PublishTree(_ context.Context, _ string, tree solrConfigTree) error {
	s.publishCalls++
	s.tree = cloneSolrConfigTree(tree)
	s.exists = true
	return nil
}

type fakeSolrConfigRuntime struct {
	t                  *testing.T
	generated          solrConfigTree
	generatedZipTar    []byte
	execCalls          [][]string
	copyToCalls        int
	stagedConfigCopies int
	coreExists         bool
	moduleEnabled      bool
	trees              map[string]solrConfigTree
	files              map[string][]byte
	directories        map[string]bool
	failSolrActions    map[string]int
	failDrush          map[string]int
	failRemove         map[string]int
	targetLockHeld     bool
}

func newFakeSolrConfigRuntime(t *testing.T, generated, runtimeTree solrConfigTree) *fakeSolrConfigRuntime {
	t.Helper()
	zipData := makeSolrConfigZip(t, []zipTestEntry{
		{name: "lang/stopwords_en.txt", data: generated["lang/stopwords_en.txt"]},
		{name: "schema.xml", data: generated["schema.xml"]},
		{name: "solrconfig.xml", data: generated["solrconfig.xml"]},
	})
	runtime := &fakeSolrConfigRuntime{
		t:               t,
		generated:       cloneSolrConfigTree(generated),
		generatedZipTar: makeSingleFileTar(t, "generated.zip", zipData),
		moduleEnabled:   true,
		trees:           map[string]solrConfigTree{},
		files:           map[string][]byte{},
		directories:     map[string]bool{},
		failSolrActions: map[string]int{},
		failDrush:       map[string]int{},
		failRemove:      map[string]int{},
	}
	corePath := path.Join(solrContainerRoot, defaultSolrCore)
	runtime.directories[corePath] = true
	if runtimeTree != nil {
		runtime.trees[path.Join(corePath, "conf")] = cloneSolrConfigTree(runtimeTree)
	}
	return runtime
}

func (r *fakeSolrConfigRuntime) AcquireLock(ctx context.Context, _, _ string) (context.Context, func() error, error) {
	if r.targetLockHeld {
		return nil, nil, fmt.Errorf("target lock already held")
	}
	r.targetLockHeld = true
	return ctx, func() error {
		r.targetLockHeld = false
		return nil
	}, nil
}

func (r *fakeSolrConfigRuntime) ExecCapture(_ context.Context, _, _ string, argv []string) (string, error) {
	r.execCalls = append(r.execCalls, append([]string(nil), argv...))
	if len(argv) == 0 {
		return "", nil
	}
	if argv[0] == "drush" {
		if len(argv) > 1 && argv[1] == "pm:list" {
			if r.moduleEnabled {
				return `{"search_api_solr":{"status":"Enabled"}}`, nil
			}
			return `{}`, nil
		}
		for _, argument := range argv {
			if r.failDrush[argument] > 0 {
				r.failDrush[argument]--
				return "", fmt.Errorf("injected drush failure for %s", argument)
			}
		}
		return "", nil
	}
	if argv[0] == "mkdir" && len(argv) == 3 && argv[1] == "-p" {
		r.addFakeDirectory(argv[2])
		return "", nil
	}
	if argv[0] == "mkdir" && len(argv) == 2 {
		if r.directories[argv[1]] {
			return "", fs.ErrExist
		}
		r.addFakeDirectory(argv[1])
		return "", nil
	}
	if argv[0] == "rm" && len(argv) == 3 && (argv[1] == "-rf" || argv[1] == "-f") {
		if r.failRemove[argv[2]] > 0 {
			r.failRemove[argv[2]]--
			return "", fmt.Errorf("injected remove failure for %s", argv[2])
		}
		r.removeFakePath(argv[2])
		return "", nil
	}
	if argv[0] == "mv" && len(argv) == 3 {
		return "", r.moveFakePath(argv[1], argv[2])
	}
	if argv[0] != "curl" {
		return "", nil
	}
	if strings.HasSuffix(argv[len(argv)-1], "/admin/info/system") {
		return `{"lucene":{"solr-spec-version":"10.0.0"}}`, nil
	}
	action := solrActionFromArgv(argv)
	if r.failSolrActions[action] > 0 {
		r.failSolrActions[action]--
		return "", fmt.Errorf("injected Solr %s failure", action)
	}
	if action == "STATUS" {
		status := `{}`
		if r.coreExists {
			status = `{"default":{"name":"default"}}`
		}
		return fmt.Sprintf(`{"responseHeader":{"status":0},"status":%s}`, status), nil
	}
	if action == "CREATE" {
		r.coreExists = true
	}
	return `{"responseHeader":{"status":0}}`, nil
}

func (r *fakeSolrConfigRuntime) CopyFrom(_ context.Context, _, sourcePath string) (io.ReadCloser, error) {
	if strings.HasPrefix(sourcePath, "/tmp/sitectl-solr-config-") {
		archive := replaceSingleTarName(r.t, r.generatedZipTar, path.Base(sourcePath))
		return io.NopCloser(bytes.NewReader(archive)), nil
	}
	if data, ok := r.files[sourcePath]; ok {
		return io.NopCloser(bytes.NewReader(makeSingleFileTar(r.t, path.Base(sourcePath), data))), nil
	}
	if tree, ok := r.trees[sourcePath]; ok {
		archive, err := buildSolrConfigTreeTar(path.Base(sourcePath), tree)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(archive)), nil
	}
	if r.directories[sourcePath] {
		archive, err := buildSolrConfigTreeTar(path.Base(sourcePath), solrConfigTree{})
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(archive)), nil
	}
	return nil, fs.ErrNotExist
}

func (r *fakeSolrConfigRuntime) CopyTo(_ context.Context, _, destination string, source io.Reader) error {
	r.copyToCalls++
	archive, err := io.ReadAll(source)
	if err != nil {
		return err
	}
	root, tree, file, err := readFakeRuntimeTar(archive)
	if err != nil {
		return err
	}
	target := path.Join(destination, root)
	if file != nil {
		r.files[target] = bytes.Clone(file)
		return nil
	}
	r.addFakeDirectory(target)
	r.trees[target] = cloneSolrConfigTree(tree)
	if root == "new" {
		r.stagedConfigCopies++
	}
	return nil
}

func (r *fakeSolrConfigRuntime) addFakeDirectory(directory string) {
	for current := path.Clean(directory); current != "." && current != "/"; current = path.Dir(current) {
		r.directories[current] = true
	}
}

func (r *fakeSolrConfigRuntime) removeFakePath(target string) {
	delete(r.files, target)
	delete(r.trees, target)
	delete(r.directories, target)
	prefix := strings.TrimSuffix(target, "/") + "/"
	for name := range r.files {
		if strings.HasPrefix(name, prefix) {
			delete(r.files, name)
		}
	}
	for name := range r.trees {
		if strings.HasPrefix(name, prefix) {
			delete(r.trees, name)
		}
	}
	for name := range r.directories {
		if strings.HasPrefix(name, prefix) {
			delete(r.directories, name)
		}
	}
}

func (r *fakeSolrConfigRuntime) moveFakePath(source, destination string) error {
	r.removeFakePath(destination)
	if data, ok := r.files[source]; ok {
		r.files[destination] = data
		delete(r.files, source)
		return nil
	}
	if tree, ok := r.trees[source]; ok {
		r.trees[destination] = tree
		delete(r.trees, source)
		delete(r.directories, source)
		r.directories[destination] = true
		r.addFakeDirectory(path.Dir(destination))
		return nil
	}
	if !r.directories[source] {
		return fs.ErrNotExist
	}
	r.directories[destination] = true
	delete(r.directories, source)
	sourcePrefix := strings.TrimSuffix(source, "/") + "/"
	destinationPrefix := strings.TrimSuffix(destination, "/") + "/"
	for name, data := range r.files {
		if strings.HasPrefix(name, sourcePrefix) {
			r.files[destinationPrefix+strings.TrimPrefix(name, sourcePrefix)] = data
			delete(r.files, name)
		}
	}
	for name, tree := range r.trees {
		if strings.HasPrefix(name, sourcePrefix) {
			r.trees[destinationPrefix+strings.TrimPrefix(name, sourcePrefix)] = tree
			delete(r.trees, name)
		}
	}
	for name := range r.directories {
		if strings.HasPrefix(name, sourcePrefix) {
			r.directories[destinationPrefix+strings.TrimPrefix(name, sourcePrefix)] = true
			delete(r.directories, name)
		}
	}
	return nil
}

func (r *fakeSolrConfigRuntime) sawDrushConfigGeneration() bool {
	for _, call := range r.execCalls {
		if len(call) >= 3 && call[0] == "drush" && call[2] == "search-api-solr:get-server-config" {
			return true
		}
	}
	return false
}

func (r *fakeSolrConfigRuntime) sawSolrAction(action string) bool {
	for _, call := range r.execCalls {
		if solrActionFromArgv(call) == action {
			return true
		}
	}
	return false
}

func (r *fakeSolrConfigRuntime) sawSolrActionCount(action string) int {
	count := 0
	for _, call := range r.execCalls {
		if solrActionFromArgv(call) == action {
			count++
		}
	}
	return count
}

func (r *fakeSolrConfigRuntime) sawExec(want []string) bool {
	for _, call := range r.execCalls {
		if reflect.DeepEqual(call, want) {
			return true
		}
	}
	return false
}

func (r *fakeSolrConfigRuntime) sawExecCount(want []string) int {
	count := 0
	for _, call := range r.execCalls {
		if reflect.DeepEqual(call, want) {
			count++
		}
	}
	return count
}

func readFakeRuntimeTar(archive []byte) (string, solrConfigTree, []byte, error) {
	reader := tar.NewReader(bytes.NewReader(archive))
	root := ""
	tree := solrConfigTree{}
	var singleFile []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, nil, err
		}
		name, err := cleanArchivePath(header.Name)
		if err != nil {
			return "", nil, nil, err
		}
		parts := strings.Split(name, "/")
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			return "", nil, nil, fmt.Errorf("fake archive has multiple roots")
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return "", nil, nil, fmt.Errorf("fake archive entry %q is not regular", name)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return "", nil, nil, err
		}
		if len(parts) == 1 {
			singleFile = append([]byte{}, data...)
			continue
		}
		tree[strings.Join(parts[1:], "/")] = append([]byte{}, data...)
	}
	if root == "" {
		return "", nil, nil, fmt.Errorf("fake archive is empty")
	}
	return root, tree, singleFile, nil
}

func solrActionFromArgv(argv []string) string {
	for index, value := range argv {
		if value == "--data-urlencode" && index+1 < len(argv) && strings.HasPrefix(argv[index+1], "action=") {
			return strings.TrimPrefix(argv[index+1], "action=")
		}
	}
	return ""
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func makeSingleFileTar(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func replaceSingleTarName(t *testing.T, archive []byte, name string) []byte {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(archive))
	header, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return makeSingleFileTar(t, name, data[:header.Size])
}
