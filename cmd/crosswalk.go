package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/libops/sitectl-drupal/pkg/endpoint"
	pluginjobs "github.com/libops/sitectl-drupal/pkg/jobs"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/helpers"
	"github.com/spf13/cobra"
)

const (
	crosswalkBinaryEnvironment       = "SITECTL_CROSSWALK_BINARY"
	maximumCrosswalkSnapshotBytes    = 64 << 20
	crosswalkProcessShutdownDeadline = 10 * time.Second
)

type crosswalkRuntime struct {
	context        func(string) (*config.Context, error)
	exportConfig   func(*cobra.Command, *config.Context) (string, func(), error)
	resolveJSONAPI func(*cobra.Command, *config.Context) (endpoint.Resolved, error)
	run            func(*cobra.Command, string, []string) error
}

func defaultCrosswalkRuntime() crosswalkRuntime {
	return crosswalkRuntime{
		context: func(contextName string) (*config.Context, error) {
			if strings.TrimSpace(contextName) != "" {
				sdk.Config.Context = contextName
			}
			return sdk.GetContext()
		},
		exportConfig:   acquireCrosswalkConfigSnapshot,
		resolveJSONAPI: endpoint.JSONAPI,
		run:            runCrosswalk,
	}
}

func newCrosswalkCmd(runtime crosswalkRuntime) *cobra.Command {
	command := &cobra.Command{
		Use:   "crosswalk",
		Short: "Run Crosswalk with metadata inputs acquired from this Drupal site",
		Long: `Acquire Drupal-owned operational inputs and invoke Crosswalk without
copying metadata models or transformations into sitectl. Config snapshots contain
only the Drupal config/sync YAML Crosswalk compiles; Drupal API credentials remain
in the environment and are never written to an artifact or added to a
child-process argument.`,
	}
	command.AddCommand(newCrosswalkProfileCmd(runtime))
	command.AddCommand(newCrosswalkServeCmd(runtime))
	return command
}

func newCrosswalkProfileCmd(runtime crosswalkRuntime) *cobra.Command {
	command := &cobra.Command{
		Use:   "profile",
		Short: "Acquire Drupal configuration for Crosswalk profile authoring",
	}
	command.AddCommand(newCrosswalkProfileCreateCmd(runtime))
	return command
}

type crosswalkProfileCreateOptions struct {
	entityType               string
	bundle                   string
	configDir                string
	output                   string
	institutionAttribute     string
	institutionScheme        string
	institutionNamespace     string
	institutionPattern       string
	institutionIdentityLevel string
}

func newCrosswalkProfileCreateCmd(runtime crosswalkRuntime) *cobra.Command {
	options := crosswalkProfileCreateOptions{
		entityType:               "node",
		output:                   "-",
		institutionAttribute:     "local",
		institutionIdentityLevel: "source_record",
	}
	command := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an editable Crosswalk profile draft from live Drupal configuration",
		Long: `Export the selected site's active configuration with Drush, capture a
bounded private config/sync archive, and ask Crosswalk to store the immutable
Drupal model and write an ordered, editable profile draft. This command does not
publish the profile. Review and edit the draft, seal it with "crosswalk profile
validate", then publish the sealed definition with "crosswalk profile publish".
Drush config export updates the site's normal config/sync directory before it is
captured.`,
		Example: `  sitectl drupal crosswalk profile create repository-items \
    --bundle islandora_object \
    --output repository-items.draft.yaml`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			return options.run(command, args[0], runtime)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.entityType, "entity-type", options.entityType, "Drupal entity type represented by the profile")
	flags.StringVar(&options.bundle, "bundle", "", "Drupal bundle represented by the profile, such as islandora_object")
	flags.StringVar(&options.configDir, "config-dir", "", "Crosswalk configuration directory used to store the immutable model")
	flags.StringVarP(&options.output, "output", "o", options.output, "Editable profile draft path, or - for stdout")
	flags.StringVar(&options.institutionAttribute, "institution-attribute", options.institutionAttribute, "Typed Drupal identifier attribute containing the institutional value")
	flags.StringVar(&options.institutionScheme, "institution-scheme", "", "Distinct machine name for the institutional identifier scheme")
	flags.StringVar(&options.institutionNamespace, "institution-namespace", "", "Absolute authority URI for the institutional identifier scheme")
	flags.StringVar(&options.institutionPattern, "institution-pattern", "", "Fully anchored Go regular expression for valid institutional identifier values")
	flags.StringVar(&options.institutionIdentityLevel, "institution-identity-level", options.institutionIdentityLevel, "Identity level: work, version, manifestation, concept, or source_record")
	must(command.MarkFlagRequired("bundle"))
	return command
}

func (options crosswalkProfileCreateOptions) run(command *cobra.Command, name string, runtime crosswalkRuntime) error {
	if err := options.validate(); err != nil {
		return err
	}
	ctx, err := runtime.context("")
	if err != nil {
		return fmt.Errorf("resolve Drupal context: %w", err)
	}
	snapshotPath, cleanup, err := runtime.exportConfig(command, ctx)
	if err != nil {
		return fmt.Errorf("acquire Drupal config snapshot from context %q: %w", contextName(ctx), err)
	}
	defer cleanup()

	arguments := []string{
		"profile", "create", "drupal", name,
		"--config", snapshotPath,
		"--entity-type", strings.TrimSpace(options.entityType),
		"--bundle", strings.TrimSpace(options.bundle),
		"--output", strings.TrimSpace(options.output),
	}
	if value := strings.TrimSpace(options.configDir); value != "" {
		arguments = append(arguments, "--config-dir", value)
	}
	if strings.TrimSpace(options.institutionScheme) != "" {
		arguments = append(arguments,
			"--institution-attribute", strings.TrimSpace(options.institutionAttribute),
			"--institution-scheme", strings.TrimSpace(options.institutionScheme),
			"--institution-namespace", strings.TrimSpace(options.institutionNamespace),
			"--institution-pattern", strings.TrimSpace(options.institutionPattern),
			"--institution-identity-level", strings.TrimSpace(options.institutionIdentityLevel),
		)
	}
	return runtime.run(command, "profile draft creation", arguments)
}

func (options crosswalkProfileCreateOptions) validate() error {
	if strings.TrimSpace(options.entityType) == "" {
		return fmt.Errorf("--entity-type is required")
	}
	if strings.TrimSpace(options.bundle) == "" {
		return fmt.Errorf("--bundle is required")
	}
	if strings.TrimSpace(options.output) == "" {
		return fmt.Errorf("--output is required; use - for stdout")
	}
	institutionalValues := []string{options.institutionScheme, options.institutionNamespace, options.institutionPattern}
	configured := 0
	for _, value := range institutionalValues {
		if strings.TrimSpace(value) != "" {
			configured++
		}
	}
	if configured != 0 && configured != len(institutionalValues) {
		return fmt.Errorf("--institution-scheme, --institution-namespace, and --institution-pattern must be supplied together")
	}
	if configured == 0 && (strings.TrimSpace(options.institutionAttribute) != "local" || strings.TrimSpace(options.institutionIdentityLevel) != "source_record") {
		return fmt.Errorf("institution attribute and identity level require an institutional scheme, namespace, and pattern")
	}
	return nil
}

func newCrosswalkServeCmd(runtime crosswalkRuntime) *cobra.Command {
	return &cobra.Command{
		Use:                "serve [CROSSWALK SERVE OPTIONS]",
		Short:              "Serve Crosswalk with this site's resolved Drupal endpoint",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		SilenceUsage:       true,
		Long: `Resolve the selected context's named Drupal JSON:API route and run
crosswalk serve with that endpoint and an explicit --drupal-profile. All other
Crosswalk serve options are forwarded unchanged. Authentication values must be
provided through Crosswalk's environment variables; sitectl neither reads nor
places them in process arguments.`,
		RunE: func(command *cobra.Command, args []string) error {
			return runCrosswalkServe(command, args, runtime)
		},
	}
}

func runCrosswalkServe(command *cobra.Command, args []string, runtime crosswalkRuntime) error {
	if hasArgument(args, "--help", "-h") {
		return command.Help()
	}
	filtered, contextNameValue, err := helpers.GetContextFromArgs(command, args)
	if err != nil {
		return fmt.Errorf("read --context: %w", err)
	}
	if optionMissingValue(args, "--context") {
		return fmt.Errorf("--context requires a value")
	}
	if hasOption(filtered, "--drupal-jsonapi") {
		return fmt.Errorf("--drupal-jsonapi is managed by sitectl; run crosswalk serve directly to use a different endpoint")
	}
	if value, ok := optionValue(filtered, "--drupal-profile"); !ok || strings.TrimSpace(value) == "" {
		return fmt.Errorf("--drupal-profile is required so repository lookup uses the model compiled for this site")
	}

	ctx, err := runtime.context(contextNameValue)
	if err != nil {
		return fmt.Errorf("resolve Drupal context: %w", err)
	}
	resolved, err := runtime.resolveJSONAPI(command, ctx)
	if err != nil {
		return fmt.Errorf("resolve Drupal JSON:API endpoint for context %q: %w", contextName(ctx), err)
	}
	if strings.TrimSpace(resolved.URL) == "" {
		return fmt.Errorf("resolve Drupal JSON:API endpoint for context %q: endpoint URL is empty", contextName(ctx))
	}
	arguments := append([]string{"serve"}, filtered...)
	arguments = append(arguments, "--drupal-jsonapi", resolved.URL)
	return runtime.run(command, "server", arguments)
}

func acquireCrosswalkConfigSnapshot(command *cobra.Command, ctx *config.Context) (string, func(), error) {
	if ctx == nil {
		return "", nil, fmt.Errorf("Drupal context is required")
	}
	file, err := os.CreateTemp("", "sitectl-drupal-crosswalk-*.tar.gz")
	if err != nil {
		return "", nil, fmt.Errorf("create private config snapshot: %w", err)
	}
	localPath := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(localPath)
	}

	if err := pluginjobs.WriteCrosswalkConfigSnapshot(command, ctx, file); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("sync config snapshot: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close config snapshot: %w", err)
	}
	info, err := os.Lstat(localPath)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("inspect config snapshot: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		cleanup()
		return "", nil, fmt.Errorf("config snapshot is not a regular file")
	}
	if info.Size() == 0 {
		cleanup()
		return "", nil, fmt.Errorf("config snapshot is empty")
	}
	if info.Size() > maximumCrosswalkSnapshotBytes {
		cleanup()
		return "", nil, fmt.Errorf("config snapshot exceeds %d bytes", maximumCrosswalkSnapshotBytes)
	}
	return localPath, cleanup, nil
}

func runCrosswalk(command *cobra.Command, operation string, arguments []string) error {
	binary := strings.TrimSpace(os.Getenv(crosswalkBinaryEnvironment))
	if binary == "" {
		binary = "crosswalk"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return fmt.Errorf("locate Crosswalk executable: install crosswalk or set %s to its path: %w", crosswalkBinaryEnvironment, err)
	}
	child := exec.CommandContext(command.Context(), path, arguments...) // #nosec G204 -- the executable is an explicit operator setting and arguments are passed without a shell.
	child.Stdin = command.InOrStdin()
	child.Stdout = command.OutOrStdout()
	child.Stderr = command.ErrOrStderr()
	child.Env = os.Environ()
	child.WaitDelay = crosswalkProcessShutdownDeadline
	child.Cancel = func() error {
		if child.Process == nil {
			return os.ErrProcessDone
		}
		return child.Process.Signal(os.Interrupt)
	}
	if err := child.Run(); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("Crosswalk %s failed: %w", operation, err)
	}
	return nil
}

func hasArgument(arguments []string, names ...string) bool {
	for _, argument := range arguments {
		for _, name := range names {
			if argument == name {
				return true
			}
		}
	}
	return false
}

func hasOption(arguments []string, name string) bool {
	_, ok := optionValue(arguments, name)
	return ok
}

func optionValue(arguments []string, name string) (string, bool) {
	for index, argument := range arguments {
		if argument == name {
			if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "-") {
				return "", true
			}
			return arguments[index+1], true
		}
		if strings.HasPrefix(argument, name+"=") {
			return strings.TrimPrefix(argument, name+"="), true
		}
	}
	return "", false
}

func optionMissingValue(arguments []string, name string) bool {
	for index, argument := range arguments {
		if argument == name && (index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "-")) {
			return true
		}
		if strings.HasPrefix(argument, name+"=") && strings.TrimSpace(strings.TrimPrefix(argument, name+"=")) == "" {
			return true
		}
	}
	return false
}

func contextName(ctx *config.Context) string {
	if ctx == nil || strings.TrimSpace(ctx.Name) == "" {
		return "<unknown>"
	}
	return ctx.Name
}
