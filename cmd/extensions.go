package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/libops/sitectl/pkg/plugin/debugui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var drupalComponentName string

const (
	cachePageWarningThreshold = int64(1 << 30)
	pageCacheExclusionURL     = "https://www.drupal.org/project/page_cache_exclusion"
)

var drupalCoreModules = map[string]struct{}{
	"action":              {},
	"announcements_feed":  {},
	"automated_cron":      {},
	"ban":                 {},
	"basic_auth":          {},
	"big_pipe":            {},
	"block":               {},
	"block_content":       {},
	"book":                {},
	"breakpoint":          {},
	"ckeditor5":           {},
	"comment":             {},
	"config":              {},
	"config_translation":  {},
	"contact":             {},
	"content_moderation":  {},
	"content_translation": {},
	"contextual":          {},
	"datetime":            {},
	"datetime_range":      {},
	"dblog":               {},
	"dynamic_page_cache":  {},
	"editor":              {},
	"field":               {},
	"field_layout":        {},
	"field_ui":            {},
	"file":                {},
	"filter":              {},
	"forum":               {},
	"help":                {},
	"help_topics":         {},
	"history":             {},
	"image":               {},
	"inline_form_errors":  {},
	"jsonapi":             {},
	"language":            {},
	"layout_builder":      {},
	"layout_discovery":    {},
	"link":                {},
	"locale":              {},
	"media":               {},
	"media_library":       {},
	"menu_link_content":   {},
	"menu_ui":             {},
	"migrate":             {},
	"migrate_drupal":      {},
	"migrate_drupal_ui":   {},
	"mysql":               {},
	"navigation":          {},
	"node":                {},
	"options":             {},
	"page_cache":          {},
	"path":                {},
	"path_alias":          {},
	"pgsql":               {},
	"phpass":              {},
	"responsive_image":    {},
	"rest":                {},
	"sdc":                 {},
	"search":              {},
	"serialization":       {},
	"settings_tray":       {},
	"shortcut":            {},
	"sqlite":              {},
	"statistics":          {},
	"syslog":              {},
	"system":              {},
	"taxonomy":            {},
	"telephone":           {},
	"text":                {},
	"toolbar":             {},
	"tour":                {},
	"tracker":             {},
	"update":              {},
	"user":                {},
	"views":               {},
	"views_ui":            {},
	"workflows":           {},
	"workspaces":          {},
}

var componentExtensionCmd = &cobra.Command{
	Use:    "__component",
	Short:  "Internal component extension command",
	Hidden: true,
}

var componentExtensionDescribeCmd = &cobra.Command{
	Use:   "describe",
	Short: "Internal component describe hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(drupalComponentName) != "" {
			return fmt.Errorf("unknown drupal component %q", drupalComponentName)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "== drupal components ==\nNo Drupal-specific components are registered yet.")
		return err
	},
}

var componentExtensionReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Internal component reconcile hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(drupalComponentName) != "" {
			return fmt.Errorf("unknown drupal component %q", drupalComponentName)
		}
		return nil
	},
}

var componentExtensionSetCmd = &cobra.Command{
	Use:   "set <name> [disposition]",
	Short: "Internal component set hook",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("unknown drupal component %q", args[0])
	},
}

// drupalDebugRunner implements plugin.DebugRunner for the drupal plugin.
type drupalDebugRunner struct {
	rootfsPath string
}

func (r *drupalDebugRunner) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&r.rootfsPath, "drupal-rootfs", "", "Drupal rootfs path override")
}

func (r *drupalDebugRunner) Render(cmd *cobra.Command, ctx *config.Context) (string, error) {
	return renderDrupalDebugBody(cmd.Context(), ctx, r.rootfsPath)
}

func init() {
	componentExtensionDescribeCmd.Flags().StringVarP(&drupalComponentName, "component", "c", "", "Specific Drupal component to describe")
	componentExtensionReconcileCmd.Flags().StringVarP(&drupalComponentName, "component", "c", "", "Specific Drupal component to reconcile")

	componentExtensionCmd.AddCommand(componentExtensionDescribeCmd)
	componentExtensionCmd.AddCommand(componentExtensionReconcileCmd)
	componentExtensionCmd.AddCommand(componentExtensionSetCmd)
}

func renderDrupalDebugBody(runCtx context.Context, ctx *config.Context, rootfsOverride string) (string, error) {
	slog.Debug("starting plugin debug", "plugin", "drupal")
	if sdk == nil {
		return "", fmt.Errorf("plugin sdk is not initialized")
	}
	slog.Debug("resolved plugin context", "plugin", "drupal", "context", ctx.Name, "project_dir", ctx.ProjectDir)
	slog.Debug("creating file accessor", "plugin", "drupal")
	files, err := sdk.GetFileAccessor()
	if err != nil {
		return "", err
	}
	defer files.Close()

	rootfs := strings.TrimSpace(rootfsOverride)
	if rootfs == "" {
		rootfs = ctx.EffectiveDrupalRootfs()
	}
	slog.Debug("resolving drupal root", "plugin", "drupal", "rootfs", rootfs)
	drupalRoot := ctx.ResolveProjectPath(rootfs)
	slog.Debug("resolved drupal root", "plugin", "drupal", "drupal_root", drupalRoot)
	configDir := filepath.Join(drupalRoot, "config", "sync")
	body := []string{
		debugui.Divider(),
		"",
		debugui.Title("General"),
		"",
		debugui.FormatRows([]debugui.Row{
			{Label: "Context", Value: ctx.Name},
			{Label: "Project dir", Value: ctx.ProjectDir},
			{Label: "Drupal root", Value: drupalRoot},
			{Label: "Config sync dir", Value: configDir},
		}),
	}

	if strings.TrimSpace(drupalRoot) == "" {
		slog.Debug("drupal root unavailable; skipping extension scan", "plugin", "drupal")
		body = append(body, "", "Installed modules: unavailable")
		return debugui.RenderPanel("drupal", strings.Join(body, "\n")), nil
	}

	slog.Debug("reading core.extension.yml", "plugin", "drupal", "path", filepath.Join(configDir, "core.extension.yml"))
	modules, themes, err := readCoreExtension(runCtx, files, filepath.Join(configDir, "core.extension.yml"))
	if err != nil {
		return "", err
	}
	moduleVersionInfo, err := readComposerLockModuleVersions(runCtx, files, drupalRoot, ctx.ProjectDir)
	if err != nil {
		return "", err
	}
	composerPatches, err := readComposerPatches(runCtx, files, drupalRoot, ctx.ProjectDir)
	if err != nil {
		return "", err
	}
	slog.Debug("read installed extensions", "plugin", "drupal", "modules", len(modules), "themes", len(themes))
	slog.Debug("rendering cache_page summary", "plugin", "drupal")
	cachePageSummary, err := renderCachePageSummary(runCtx)
	if err != nil {
		body = append(body, "", debugui.Divider(), "", debugui.Title("Cache Page"), "", debugui.FormatRows([]debugui.Row{
			{Label: "Status", Value: debugui.Status("warning")},
			{Label: "cache_page", Value: fmt.Sprintf("unavailable (%v)", err)},
		}))
	} else if strings.TrimSpace(cachePageSummary) != "" {
		body = append(body, "", debugui.Divider(), "", debugui.Title("Cache Page"), "", cachePageSummary)
	}

	moduleLines, err := renderModuleList(runCtx, files, drupalRoot, modules, moduleVersionInfo)
	if err != nil {
		return "", err
	}

	configLines := []string{debugui.Divider(), "", debugui.Title("Installed Extensions"), "", fmt.Sprintf("Installed modules (%d):", len(modules))}
	configLines = append(configLines, moduleLines...)
	configLines = append(configLines, "")
	configLines = append(configLines, fmt.Sprintf("Installed themes (%d):", len(themes)))
	configLines = append(configLines, formatListLines(themes, 3)...)
	body = append(body, "", strings.Join(configLines, "\n"))

	patchLines := []string{debugui.Divider(), "", debugui.Title("Composer Patches"), ""}
	if strings.TrimSpace(composerPatches) == "" {
		patchLines = append(patchLines, "  none")
	} else {
		patchLines = append(patchLines, indentLines(composerPatches, "  "))
	}
	body = append(body, "", strings.Join(patchLines, "\n"))

	slog.Debug("finished plugin debug body", "plugin", "drupal")
	return strings.Join(body, "\n"), nil
}

func renderCachePageSummary(runCtx context.Context) (string, error) {
	ctx, cli, containerName, err := getDrupalContainerForSDK(runCtx)
	if err != nil {
		return "", err
	}
	defer cli.Close()

	cachePageSize, err := readDrupalCacheTableSize(runCtx, cli, containerName, ctx.EffectiveDrupalContainerRoot(), "cache_page")
	if err != nil {
		return "", err
	}
	cacheRenderSize, err := readDrupalCacheTableSize(runCtx, cli, containerName, ctx.EffectiveDrupalContainerRoot(), "cache_render")
	if err != nil {
		return "", err
	}

	rows := []debugui.Row{
		{Label: "Status", Value: debugui.Status("ok")},
		{Label: "cache_page", Value: humanBytes(cachePageSize)},
		{Label: "cache_render", Value: humanBytes(cacheRenderSize)},
	}
	if cachePageSize >= cachePageWarningThreshold || cacheRenderSize >= cachePageWarningThreshold {
		rows[0].Value = debugui.Status("warning")
	}
	if cachePageSize >= cachePageWarningThreshold {
		rows = append(rows, debugui.Row{Label: "Recommendation", Value: pageCacheExclusionURL})
	}
	return debugui.FormatRows(rows), nil
}

func readDrupalCacheTableSize(runCtx context.Context, cli *docker.DockerClient, containerName, containerRoot, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COALESCE(data_length + index_length, 0) FROM information_schema.TABLES WHERE table_schema = DATABASE() AND table_name = '%s';", strings.TrimSpace(tableName))
	cmd := []string{"drush", "sql:query", query, "--extra=--batch", "--extra=--skip-column-names"}
	slog.Debug(strings.Join(cmd, " "), "plugin", "drupal", "container", containerName)
	output, err := docker.ExecCapture(runCtx, cli, containerName, containerRoot, cmd)
	if err != nil {
		return 0, err
	}
	return parseFirstInt(output)
}

func getDrupalContainerForSDK(runCtx context.Context) (ctx *config.Context, cli *docker.DockerClient, containerName string, err error) {
	if sdk == nil {
		return nil, nil, "", fmt.Errorf("plugin sdk is not initialized")
	}

	ctx, err = sdk.GetContext()
	if err != nil {
		return nil, nil, "", err
	}

	cli, err = sdk.GetDockerClient()
	if err != nil {
		return nil, nil, "", err
	}

	containerName, err = cli.GetContainerNameContext(runCtx, ctx, *drupalServiceName)
	if err != nil {
		if closeErr := cli.Close(); closeErr != nil {
			err = fmt.Errorf("%w; close docker client: %v", err, closeErr)
		}
		return nil, nil, "", err
	}

	return ctx, cli, containerName, nil
}

func parseFirstInt(output string) (int64, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strconv.ParseInt(line, 10, 64)
	}
	return 0, fmt.Errorf("no numeric output returned")
}

func humanBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

func readCoreExtension(runCtx context.Context, files *plugin.FileAccessor, path string) ([]string, []string, error) {
	data, err := files.ReadFileContext(runCtx, path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	var extension struct {
		Module map[string]any `yaml:"module"`
		Theme  map[string]any `yaml:"theme"`
	}
	if err := yaml.Unmarshal(data, &extension); err != nil {
		return nil, nil, err
	}

	modules := make([]string, 0, len(extension.Module))
	for name := range extension.Module {
		modules = append(modules, name)
	}
	sort.Strings(modules)

	themes := make([]string, 0, len(extension.Theme))
	for name := range extension.Theme {
		themes = append(themes, name)
	}
	sort.Strings(themes)

	return modules, themes, nil
}

func readComposerLockModuleVersions(runCtx context.Context, files *plugin.FileAccessor, drupalRoot, projectDir string) (composerLockVersionInfo, error) {
	path, err := findComposerLockPath(runCtx, files, drupalRoot, projectDir)
	if err != nil {
		return composerLockVersionInfo{}, err
	}
	if path == "" {
		return composerLockVersionInfo{}, nil
	}

	data, err := files.ReadFileContext(runCtx, path)
	if err != nil {
		if os.IsNotExist(err) {
			return composerLockVersionInfo{}, nil
		}
		return composerLockVersionInfo{}, err
	}

	var lock struct {
		Packages    []composerLockPackage `json:"packages"`
		PackagesDev []composerLockPackage `json:"packages-dev"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return composerLockVersionInfo{}, err
	}

	info := composerLockVersionInfo{ModuleVersions: make(map[string]string)}
	for _, pkg := range append(lock.Packages, lock.PackagesDev...) {
		name, version := strings.TrimSpace(pkg.Name), strings.TrimSpace(pkg.Version)
		if version == "" {
			continue
		}
		switch name {
		case "drupal/core", "drupal/drupal":
			if info.CoreVersion == "" {
				info.CoreVersion = version
			}
		}
		if pkg.Type != "drupal-module" {
			continue
		}
		moduleName := strings.TrimSpace(pkg.ModuleName())
		if moduleName == "" {
			continue
		}
		info.ModuleVersions[moduleName] = version
	}
	return info, nil
}

func readComposerPatches(runCtx context.Context, files *plugin.FileAccessor, drupalRoot, projectDir string) (string, error) {
	path, err := findComposerFilePath(runCtx, files, drupalRoot, projectDir, "composer.json")
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}

	data, err := files.ReadFileContext(runCtx, path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var composer struct {
		Extra struct {
			Patches json.RawMessage `json:"patches"`
		} `json:"extra"`
	}
	if err := json.Unmarshal(data, &composer); err != nil {
		return "", err
	}
	if len(composer.Extra.Patches) == 0 || string(composer.Extra.Patches) == "null" {
		return "", nil
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, composer.Extra.Patches, "", "  "); err != nil {
		return "", err
	}
	return pretty.String(), nil
}

func findComposerLockPath(runCtx context.Context, files *plugin.FileAccessor, drupalRoot, projectDir string) (string, error) {
	return findComposerFilePath(runCtx, files, drupalRoot, projectDir, "composer.lock")
}

func findComposerFilePath(runCtx context.Context, files *plugin.FileAccessor, drupalRoot, projectDir, fileName string) (string, error) {
	candidates := []string{
		filepath.Join(strings.TrimSpace(drupalRoot), fileName),
	}
	if projectDir != drupalRoot {
		candidates = append(candidates, filepath.Join(strings.TrimSpace(projectDir), fileName))
	}

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := files.ReadFileContext(runCtx, candidate); err == nil {
			return candidate, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", nil
}

type composerLockVersionInfo struct {
	CoreVersion    string
	ModuleVersions map[string]string
}

type composerLockPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Type    string `json:"type"`
}

func (p composerLockPackage) ModuleName() string {
	name := strings.TrimSpace(p.Name)
	if !strings.HasPrefix(name, "drupal/") {
		return ""
	}
	return strings.TrimPrefix(name, "drupal/")
}

func renderModuleList(runCtx context.Context, files *plugin.FileAccessor, drupalRoot string, modules []string, versionInfo composerLockVersionInfo) ([]string, error) {
	coreModules := make([]string, 0)
	contribModules := make([]string, 0)
	unknownModules := make([]string, 0)

	for _, module := range modules {
		module = strings.TrimSpace(module)
		if module == "" {
			continue
		}
		if isStaticCoreDrupalModule(module) {
			if version := strings.TrimSpace(versionInfo.CoreVersion); version != "" {
				coreModules = append(coreModules, fmt.Sprintf("%s@%s", module, version))
			} else {
				coreModules = append(coreModules, module)
			}
			continue
		}
		if version := strings.TrimSpace(versionInfo.ModuleVersions[module]); version != "" {
			contribModules = append(contribModules, fmt.Sprintf("%s@%s", module, version))
			continue
		}
		unknownModules = append(unknownModules, module)
	}

	sort.Strings(coreModules)
	sort.Strings(contribModules)
	sort.Strings(unknownModules)

	sections := []struct {
		Title  string
		Values []string
	}{
		{Title: "Core modules:", Values: coreModules},
		{Title: "Contrib modules:", Values: contribModules},
		{Title: "Custom or submodules:", Values: unknownModules},
	}

	lines := make([]string, 0)
	for idx, section := range sections {
		lines = append(lines, "  "+section.Title)
		lines = append(lines, formatListLines(section.Values, 3)...)
		if idx < len(sections)-1 {
			lines = append(lines, "")
		}
	}
	return lines, nil
}

func isStaticCoreDrupalModule(module string) bool {
	_, ok := drupalCoreModules[strings.TrimSpace(module)]
	return ok
}

func indentLines(value, prefix string) string {
	if strings.TrimSpace(value) == "" {
		return strings.TrimSpace(prefix)
	}
	parts := strings.Split(value, "\n")
	for i, part := range parts {
		parts[i] = prefix + part
	}
	return strings.Join(parts, "\n")
}

func formatListLines(values []string, perLine int) []string {
	if len(values) == 0 {
		return []string{"  none"}
	}
	if perLine <= 0 {
		perLine = 10
	}

	lines := make([]string, 0, (len(values)+perLine-1)/perLine)
	for i := 0; i < len(values); i += perLine {
		end := i + perLine
		if end > len(values) {
			end = len(values)
		}
		lines = append(lines, "  "+strings.Join(values[i:end], ", "))
	}
	return lines
}
