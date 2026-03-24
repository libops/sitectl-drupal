package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/helpers"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

const (
	drupalCreateRepo       = "https://github.com/libops/drupal"
	drupalCreateBranch     = "main"
	drupalCreateDrupalRoot = "."
	drupalContainerRoot    = "/var/www/drupal"
)

var (
	drupalCreateInput             = config.GetInput
	drupalCreateCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		return sdk.CloneTemplateRepo(opts)
	}
	drupalCreateRunShellCommand = runCreateShellCommand
)

type createRunner struct{}

type createRequest struct {
	plugin.ComposeCreateRequest
}

func (createRunner) BindFlags(cmd *cobra.Command) {
	if err := sdk.BindComposeCreateFlags(cmd, createDefinition(), nil, ""); err != nil {
		panic(err)
	}
}

func (createRunner) Run(cmd *cobra.Command) error {
	if sdk == nil {
		return fmt.Errorf("plugin sdk is not initialized")
	}
	req, err := resolveCreateRequest(cmd)
	if err != nil {
		return err
	}
	ctx, err := ensureCreateContext(req)
	if err != nil {
		return err
	}
	cloned, err := ensureDrupalCheckout(cmd.OutOrStdout(), req, ctx)
	if err != nil {
		return err
	}
	if cloned {
		if err := runCommandList(cmd, ctx, createDefinition().DockerComposeInit); err != nil {
			return err
		}
	}
	if !req.SetupOnly {
		if err := runCommandList(cmd, ctx, createDefinition().DockerComposeUp); err != nil {
			return err
		}
	}
	printCreateSummary(cmd.OutOrStdout(), ctx, req)
	return nil
}

func createDefinition() plugin.CreateSpec {
	return plugin.CreateSpec{
		Name:                "default",
		Description:         "Create a Docker Compose Drupal stack",
		Default:             true,
		MinCPUCores:         2,
		MinMemory:           "4 GiB",
		MinDiskSpace:        "20 GiB",
		DockerComposeRepo:   drupalCreateRepo,
		DockerComposeBranch: drupalCreateBranch,
		DockerComposeUp:     []string{"docker compose up --remove-orphans"},
		DockerComposeDown:   []string{"docker compose down"},
	}
}

func resolveCreateRequest(cmd *cobra.Command) (createRequest, error) {
	resolved, err := sdk.ResolveComposeCreateRequest(cmd, drupalCreateInput, "", "", drupalCreateRepo, drupalCreateBranch)
	if err != nil {
		return createRequest{}, err
	}
	return createRequest{ComposeCreateRequest: resolved}, nil
}

func ensureCreateContext(req createRequest) (*config.Context, error) {
	defaultDir := helpers.FirstNonEmpty(req.Path, "./drupal")
	defaultName := filepath.Base(defaultDir) + "-local"
	return sdk.EnsureComposeCreateContext(req.ComposeCreateRequest, plugin.ComposeCreateContextOptions{
		DefaultName:         defaultName,
		DefaultSite:         filepath.Base(defaultDir),
		DefaultPlugin:       "drupal",
		DefaultProjectDir:   defaultDir,
		DefaultProjectName:  filepath.Base(defaultDir),
		DefaultEnvironment:  "local",
		DefaultDrupalRootfs: drupalCreateDrupalRoot,
		DrupalContainerRoot: drupalContainerRoot,
		Input:               drupalCreateInput,
	})
}

func ensureDrupalCheckout(out io.Writer, req createRequest, ctx *config.Context) (bool, error) {
	if req.CheckoutSource == plugin.CheckoutSourceExisting {
		return false, nil
	}
	if req.TemplateRepo == "" {
		return false, fmt.Errorf("template repo cannot be empty")
	}
	if ctx == nil || ctx.ProjectDir == "" {
		return false, fmt.Errorf("project directory cannot be empty")
	}
	if ctx.DockerHostType == config.ContextRemote {
		return ensureRemoteDrupalCheckout(out, req, ctx)
	}
	return ensureLocalDrupalCheckout(out, req, ctx.ProjectDir)
}

func ensureLocalDrupalCheckout(out io.Writer, req createRequest, projectDir string) (bool, error) {
	entries, err := os.ReadDir(projectDir)
	if err == nil && len(entries) > 0 {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read project directory %q: %w", projectDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(projectDir), 0o755); err != nil {
		return false, fmt.Errorf("create parent directory for %q: %w", projectDir, err)
	}
	fmt.Fprintf(out, "Cloning %s (%s) into %s\n", req.TemplateRepo, helpers.FirstNonEmpty(req.TemplateBranch, "default branch"), projectDir)
	if err := drupalCreateCloneTemplateRepo(plugin.GitTemplateOptions{
		TemplateRepo:   req.TemplateRepo,
		TemplateBranch: req.TemplateBranch,
		ProjectDir:     projectDir,
		Quiet:          true,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func ensureRemoteDrupalCheckout(out io.Writer, req createRequest, ctx *config.Context) (bool, error) {
	checkCmd := exec.Command("bash", "-lc", fmt.Sprintf("if [ -d %s ] && [ -n \"$(ls -A %s 2>/dev/null)\" ]; then echo present; fi", shellQuote(ctx.ProjectDir), shellQuote(ctx.ProjectDir)))
	output, err := ctx.RunCommand(checkCmd)
	if err == nil && strings.TrimSpace(output) == "present" {
		return false, nil
	}
	mkdirCmd := exec.Command("bash", "-lc", fmt.Sprintf("mkdir -p %s", shellQuote(filepath.Dir(ctx.ProjectDir))))
	if _, err := ctx.RunCommand(mkdirCmd); err != nil {
		return false, fmt.Errorf("prepare remote parent directory: %w", err)
	}
	cloneCmd := fmt.Sprintf("git clone --branch %s %s %s && rm -rf %s/.git && git -C %s init -b %s", shellQuote(req.TemplateBranch), shellQuote(req.TemplateRepo), shellQuote(ctx.ProjectDir), shellQuote(ctx.ProjectDir), shellQuote(ctx.ProjectDir), shellQuote(req.TemplateBranch))
	fmt.Fprintf(out, "Cloning %s (%s) into %s on %s\n", req.TemplateRepo, helpers.FirstNonEmpty(req.TemplateBranch, "default branch"), ctx.ProjectDir, ctx.SSHHostname)
	if err := drupalCreateRunShellCommand(ctx, "", io.Discard, io.Discard, cloneCmd); err != nil {
		return false, err
	}
	return true, nil
}

func runCommandList(cmd *cobra.Command, ctx *config.Context, commands []string) error {
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Running %s\n", command)
		if err := drupalCreateRunShellCommand(ctx, ctx.ProjectDir, cmd.OutOrStdout(), cmd.ErrOrStderr(), command); err != nil {
			return err
		}
	}
	return nil
}

func runCreateShellCommand(ctx *config.Context, projectDir string, stdout, stderr io.Writer, command string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if ctx.DockerHostType == config.ContextRemote {
		remoteCommand := command
		if strings.TrimSpace(projectDir) != "" {
			remoteCommand = fmt.Sprintf("cd %s && %s", shellQuote(projectDir), command)
		}
		output, err := ctx.RunCommand(exec.Command("bash", "-lc", remoteCommand))
		if strings.TrimSpace(output) != "" && stdout != nil {
			_, _ = io.WriteString(stdout, output)
			if !strings.HasSuffix(output, "\n") {
				_, _ = io.WriteString(stdout, "\n")
			}
		}
		if err != nil {
			return err
		}
		return nil
	}
	localCmd := exec.Command("bash", "-lc", command)
	localCmd.Dir = projectDir
	localCmd.Stdout = stdout
	localCmd.Stderr = stderr
	localCmd.Env = os.Environ()
	return localCmd.Run()
}

func printCreateSummary(out io.Writer, ctx *config.Context, req createRequest) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, corecomponent.RenderSection("Create complete", "Drupal is ready for use through sitectl."))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Checkout: %s\n", ctx.ProjectDir)
	fmt.Fprintf(out, "Context:  %s\n", ctx.Name)
	fmt.Fprintf(out, "Target:   %s\n", ctx.DockerHostType)
	if req.SetupOnly {
		fmt.Fprintln(out, "The stack was prepared but left stopped because --setup-only was used.")
	} else {
		fmt.Fprintln(out, "The stack was prepared and started.")
	}
	fmt.Fprintln(out)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
