package cmd

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var drupalComponentName string
var drupalRootfsPath string

const (
	cachePageWarningThreshold = int64(1 << 30)
	pageCacheExclusionURL     = "https://www.drupal.org/project/page_cache_exclusion"
)

var (
	debugPanelStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#112235")).
			Padding(1, 2)
	debugTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#98C1D9"))
	debugSectionDividerStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#29425E"))
	debugStatusOKStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#7BD389"))
	debugStatusWarningStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F4C95D"))
	debugMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9FB3C8"))
	debugRowStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#112235"))
)

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

var debugExtensionCmd = &cobra.Command{
	Use:    "__debug",
	Short:  "Internal debug extension command",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		rendered, err := renderDrupalDebug(cmd.Context())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), rendered)
		return err
	},
}

func init() {
	componentExtensionDescribeCmd.Flags().StringVarP(&drupalComponentName, "component", "c", "", "Specific Drupal component to describe")
	componentExtensionReconcileCmd.Flags().StringVarP(&drupalComponentName, "component", "c", "", "Specific Drupal component to reconcile")

	componentExtensionCmd.AddCommand(componentExtensionDescribeCmd)
	componentExtensionCmd.AddCommand(componentExtensionReconcileCmd)
	componentExtensionCmd.AddCommand(componentExtensionSetCmd)

	debugExtensionCmd.Flags().StringVar(&drupalRootfsPath, "drupal-rootfs", "", "Drupal rootfs path override")
}

func renderDrupalDebug(runCtx context.Context) (string, error) {
	slog.Debug("starting plugin debug", "plugin", "drupal")
	if sdk == nil {
		return "", fmt.Errorf("plugin sdk is not initialized")
	}
	ctx, err := sdk.GetContext()
	if err != nil {
		return "", err
	}
	slog.Debug("resolved plugin context", "plugin", "drupal", "context", ctx.Name, "project_dir", ctx.ProjectDir)
	slog.Debug("creating file accessor", "plugin", "drupal")
	files, err := sdk.GetFileAccessor()
	if err != nil {
		return "", err
	}
	defer files.Close()

	rootfs := strings.TrimSpace(drupalRootfsPath)
	if rootfs == "" {
		rootfs = ctx.EffectiveDrupalRootfs()
	}
	slog.Debug("resolving drupal root", "plugin", "drupal", "rootfs", rootfs)
	drupalRoot := ctx.ResolveProjectPath(rootfs)
	slog.Debug("resolved drupal root", "plugin", "drupal", "drupal_root", drupalRoot)
	configDir := filepath.Join(drupalRoot, "config", "sync")
	body := []string{
		debugDivider(),
		"",
		debugTitleStyle.Render("General"),
		"",
		formatDebugRows([]debugRow{
			{Label: "Context", Value: ctx.Name},
			{Label: "Project dir", Value: ctx.ProjectDir},
			{Label: "Drupal root", Value: drupalRoot},
			{Label: "Config sync dir", Value: configDir},
		}),
	}

	if strings.TrimSpace(drupalRoot) == "" {
		slog.Debug("drupal root unavailable; skipping extension scan", "plugin", "drupal")
		body = append(body, "", "Installed modules: unavailable")
		return renderDebugPanel("drupal", strings.Join(body, "\n")), nil
	}

	slog.Debug("reading core.extension.yml", "plugin", "drupal", "path", filepath.Join(configDir, "core.extension.yml"))
	modules, themes, err := readCoreExtension(runCtx, files, filepath.Join(configDir, "core.extension.yml"))
	if err != nil {
		return "", err
	}
	slog.Debug("read installed extensions", "plugin", "drupal", "modules", len(modules), "themes", len(themes))
	slog.Debug("rendering cache_page summary", "plugin", "drupal")
	cachePageSummary, err := renderCachePageSummary(runCtx)
	if err != nil {
		body = append(body, "", debugDivider(), "", debugTitleStyle.Render("Cache Page"), "", formatDebugRows([]debugRow{
			{Label: "Status", Value: renderStatus("warning")},
			{Label: "cache_page", Value: fmt.Sprintf("unavailable (%v)", err)},
		}))
	} else if strings.TrimSpace(cachePageSummary) != "" {
		body = append(body, "", debugDivider(), "", debugTitleStyle.Render("Cache Page"), "", cachePageSummary)
	}

	configLines := []string{debugDivider(), "", debugTitleStyle.Render("Installed Extensions"), "", fmt.Sprintf("Installed modules (%d):", len(modules))}
	configLines = append(configLines, formatListLines(modules, 3)...)
	configLines = append(configLines, "")
	configLines = append(configLines, fmt.Sprintf("Installed themes (%d):", len(themes)))
	configLines = append(configLines, formatListLines(themes, 3)...)
	body = append(body, "", strings.Join(configLines, "\n"))

	slog.Debug("finished plugin debug", "plugin", "drupal")
	return renderDebugPanel("drupal", strings.Join(body, "\n")), nil
}

func renderCachePageSummary(runCtx context.Context) (string, error) {
	ctx, cli, containerName, err := getDrupalContainerForSDK(runCtx)
	if err != nil {
		return "", err
	}
	defer cli.Close()

	query := "SELECT COALESCE(data_length + index_length, 0) FROM information_schema.TABLES WHERE table_schema = DATABASE() AND table_name = 'cache_page';"
	output, err := execDrupalCommandCapture(runCtx, cli, containerName, ctx.EffectiveDrupalContainerRoot(), []string{"drush", "sql:query", query, "--extra=--batch", "--extra=--skip-column-names"})
	if err != nil {
		return "", err
	}

	size, err := parseFirstInt(output)
	if err != nil {
		return "", err
	}

	rows := []debugRow{
		{Label: "Status", Value: renderStatus("ok")},
		{Label: "cache_page", Value: humanBytes(size)},
	}
	if size >= cachePageWarningThreshold {
		rows[0].Value = renderStatus("warning")
		rows = append(rows, debugRow{Label: "Recommendation", Value: pageCacheExclusionURL})
	}
	return formatDebugRows(rows), nil
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
		cli.Close()
		return nil, nil, "", err
	}

	return ctx, cli, containerName, nil
}

func execDrupalCommandCapture(runCtx context.Context, cli *docker.DockerClient, containerName, containerRoot string, cmd []string) (string, error) {
	slog.Debug(strings.Join(cmd, " "), "plugin", "drupal", "container", containerName)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := cli.Exec(runCtx, docker.ExecOptions{
		Container:    containerName,
		Cmd:          cmd,
		WorkingDir:   containerRoot,
		AttachStdout: true,
		AttachStderr: true,
		Stdout:       &stdout,
		Stderr:       &stderr,
	})
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return "", fmt.Errorf("drupal command failed with exit code %d: %s", exitCode, detail)
		}
		return "", fmt.Errorf("drupal command failed with exit code %d", exitCode)
	}

	return strings.TrimSpace(stdout.String()), nil
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
		Module map[string]int `yaml:"module"`
		Theme  map[string]int `yaml:"theme"`
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

type debugRow struct {
	Label string
	Value string
}

func renderDebugPanel(title, body string) string {
	header := debugTitleStyle.Render(strings.TrimSpace(title))
	content := header
	if strings.TrimSpace(body) != "" {
		content += "\n\n" + body
	}
	return debugPanelStyle.Width(debugPanelWidth()).Render(content)
}

func formatDebugRows(rows []debugRow) string {
	labelWidth := 0
	for _, row := range rows {
		if width := len(strings.TrimSpace(row.Label)); width > labelWidth {
			labelWidth = width
		}
	}
	lines := make([]string, 0, len(rows))
	rowWidth := debugContentWidth()
	for _, row := range rows {
		label := strings.TrimSpace(row.Label)
		value := strings.TrimSpace(row.Value)
		if label == "" {
			lines = append(lines, renderDebugRow(rowWidth, "", value))
			continue
		}
		lines = append(lines, renderDebugRow(rowWidth, fmt.Sprintf("%-*s", labelWidth, label), value))
	}
	return strings.Join(lines, "\n")
}

func renderStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "ok":
		return debugStatusOKStyle.Render("OK")
	case "warning":
		return debugStatusWarningStyle.Render("WARNING")
	default:
		return debugMutedStyle.Render(strings.ToUpper(strings.TrimSpace(state)))
	}
}

func renderDebugRow(width int, label, value string) string {
	valueWidth := max(0, width-lipgloss.Width(label)-2)
	row := label
	if strings.TrimSpace(label) != "" {
		row += "  "
	}
	row += lipgloss.NewStyle().
		Width(valueWidth).
		Background(lipgloss.Color("#112235")).
		Render(value)
	return debugRowStyle.Width(width).Render(row)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func debugPanelWidth() int {
	if columns, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && columns > 0 {
		return max(40, columns)
	}
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		return max(40, width)
	}
	return 100
}

func debugContentWidth() int {
	return max(20, debugPanelWidth()-4)
}

func debugDivider() string {
	return debugSectionDividerStyle.Width(debugContentWidth()).Render(strings.Repeat("─", debugContentWidth()))
}
