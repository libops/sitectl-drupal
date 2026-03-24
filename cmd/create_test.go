package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

func TestCreateDefinition(t *testing.T) {
	spec := createDefinition()
	if spec.Name != "default" {
		t.Fatalf("expected default definition name, got %q", spec.Name)
	}
	if spec.DockerComposeRepo != drupalCreateRepo {
		t.Fatalf("expected repo %q, got %q", drupalCreateRepo, spec.DockerComposeRepo)
	}
	if spec.DockerComposeBranch != drupalCreateBranch {
		t.Fatalf("expected branch %q, got %q", drupalCreateBranch, spec.DockerComposeBranch)
	}
	if !spec.Default {
		t.Fatal("expected Drupal create definition to be the default")
	}
}

func TestResolveCreateRequestHonorsExplicitFlags(t *testing.T) {
	oldSDK := sdk
	t.Cleanup(func() { sdk = oldSDK })
	sdk = plugin.NewSDK(plugin.Metadata{Name: "drupal"})

	cmd := &cobra.Command{Use: "default"}
	cmd.Flags().String("context", "", "")
	if err := sdk.BindComposeCreateFlags(cmd, createDefinition(), nil, ""); err != nil {
		t.Fatalf("BindComposeCreateFlags() error = %v", err)
	}
	_ = cmd.Flags().Set("context", "drupal-local")
	_ = cmd.Flags().Set("type", "local")
	_ = cmd.Flags().Set("checkout-source", "existing")
	_ = cmd.Flags().Set("project-dir", "/tmp/drupal")
	_ = cmd.Flags().Set("setup-only", "true")

	req, err := resolveCreateRequest(cmd)
	if err != nil {
		t.Fatalf("resolveCreateRequest() error = %v", err)
	}
	if req.ContextName != "drupal-local" {
		t.Fatalf("expected context name drupal-local, got %q", req.ContextName)
	}
	if req.Path != "/tmp/drupal" {
		t.Fatalf("expected path /tmp/drupal, got %q", req.Path)
	}
	if req.TargetType != config.ContextLocal {
		t.Fatalf("expected local target, got %q", req.TargetType)
	}
	if req.CheckoutSource != plugin.CheckoutSourceExisting {
		t.Fatalf("expected existing checkout source, got %q", req.CheckoutSource)
	}
	if !req.SetupOnly {
		t.Fatal("expected setup-only request")
	}
}

func TestEnsureLocalDrupalCheckoutClonesEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "site")

	oldClone := drupalCreateCloneTemplateRepo
	t.Cleanup(func() { drupalCreateCloneTemplateRepo = oldClone })

	var cloneInvoked bool
	drupalCreateCloneTemplateRepo = func(opts plugin.GitTemplateOptions) error {
		cloneInvoked = true
		if opts.TemplateRepo != drupalCreateRepo {
			t.Fatalf("expected repo %q, got %q", drupalCreateRepo, opts.TemplateRepo)
		}
		if opts.TemplateBranch != drupalCreateBranch {
			t.Fatalf("expected branch %q, got %q", drupalCreateBranch, opts.TemplateBranch)
		}
		if opts.ProjectDir != projectDir {
			t.Fatalf("expected project dir %q, got %q", projectDir, opts.ProjectDir)
		}
		return os.MkdirAll(opts.ProjectDir, 0o755)
	}

	cloned, err := ensureLocalDrupalCheckout(ioDiscard{}, createRequest{ComposeCreateRequest: plugin.ComposeCreateRequest{
		TemplateRepo:   drupalCreateRepo,
		TemplateBranch: drupalCreateBranch,
	}}, projectDir)
	if err != nil {
		t.Fatalf("ensureLocalDrupalCheckout() error = %v", err)
	}
	if !cloned {
		t.Fatal("expected checkout to be cloned")
	}
	if !cloneInvoked {
		t.Fatal("expected clone to run")
	}
}

func TestPrintCreateSummarySetupOnly(t *testing.T) {
	var out bytes.Buffer
	printCreateSummary(&out, &config.Context{Name: "drupal-local", ProjectDir: "/tmp/drupal", DockerHostType: config.ContextLocal}, createRequest{
		ComposeCreateRequest: plugin.ComposeCreateRequest{SetupOnly: true},
	})
	if !strings.Contains(out.String(), "left stopped because --setup-only was used") {
		t.Fatalf("expected setup-only summary, got:\n%s", out.String())
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
