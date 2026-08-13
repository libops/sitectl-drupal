package cmd

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/libops/sitectl-drupal/pkg/endpoint"
	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

func TestCrosswalkProfileCreateAcquiresSnapshotAndInvokesCanonicalCLI(t *testing.T) {
	t.Parallel()

	var gotOperation string
	var gotArguments []string
	cleaned := false
	runtime := crosswalkRuntime{
		context: func(name string) (*config.Context, error) {
			if name != "" {
				t.Fatalf("context name = %q, want current context", name)
			}
			return &config.Context{Name: "repository"}, nil
		},
		exportConfig: func(_ *cobra.Command, ctx *config.Context) (string, func(), error) {
			if ctx.Name != "repository" {
				t.Fatalf("export context = %+v", ctx)
			}
			return "/private/config.tar.gz", func() { cleaned = true }, nil
		},
		run: func(_ *cobra.Command, operation string, arguments []string) error {
			gotOperation = operation
			gotArguments = append([]string(nil), arguments...)
			return nil
		},
	}
	command := newCrosswalkCmd(runtime)
	command.SetArgs([]string{
		"profile", "create", "repository-items",
		"--entity-type", "node",
		"--bundle", "islandora_object",
		"--config-dir", "/profiles",
		"--output", "/work/repository-items.draft.yaml",
		"--institution-attribute", "local",
		"--institution-scheme", "lehigh-id",
		"--institution-namespace", "https://id.example.edu/items/",
		"--institution-pattern", `^[0-9]+$`,
		"--institution-identity-level", "source_record",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !cleaned {
		t.Fatal("config snapshot cleanup was not called")
	}
	if gotOperation != "profile draft creation" {
		t.Fatalf("operation = %q", gotOperation)
	}
	wantArguments := []string{
		"profile", "create", "drupal", "repository-items",
		"--config", "/private/config.tar.gz",
		"--entity-type", "node",
		"--bundle", "islandora_object",
		"--output", "/work/repository-items.draft.yaml",
		"--config-dir", "/profiles",
		"--institution-attribute", "local",
		"--institution-scheme", "lehigh-id",
		"--institution-namespace", "https://id.example.edu/items/",
		"--institution-pattern", `^[0-9]+$`,
		"--institution-identity-level", "source_record",
	}
	if !reflect.DeepEqual(gotArguments, wantArguments) {
		t.Fatalf("Crosswalk arguments = %#v, want %#v", gotArguments, wantArguments)
	}
}

func TestCrosswalkProfileCreateDefaultsDraftToStdout(t *testing.T) {
	t.Parallel()

	var gotArguments []string
	runtime := crosswalkRuntime{
		context: func(string) (*config.Context, error) { return &config.Context{Name: "repository"}, nil },
		exportConfig: func(*cobra.Command, *config.Context) (string, func(), error) {
			return "/private/config.tar.gz", func() {}, nil
		},
		run: func(_ *cobra.Command, _ string, arguments []string) error {
			gotArguments = append([]string(nil), arguments...)
			return nil
		},
	}
	command := newCrosswalkCmd(runtime)
	command.SetArgs([]string{"profile", "create", "repository-items", "--bundle", "islandora_object"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	outputToStdout := false
	for index := 0; index+1 < len(gotArguments); index++ {
		if gotArguments[index] == "--output" && gotArguments[index+1] == "-" {
			outputToStdout = true
			break
		}
	}
	if !outputToStdout {
		t.Fatalf("Crosswalk arguments = %#v, want --output -", gotArguments)
	}
	if hasOption(gotArguments, "--force") {
		t.Fatalf("Crosswalk arguments unexpectedly publish or replace a profile: %#v", gotArguments)
	}
}

func TestCrosswalkProfileCreateCleansSnapshotAfterFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("profile rejected")
	cleaned := false
	runtime := crosswalkRuntime{
		context: func(string) (*config.Context, error) { return &config.Context{Name: "repository"}, nil },
		exportConfig: func(*cobra.Command, *config.Context) (string, func(), error) {
			return "/private/config.tar.gz", func() { cleaned = true }, nil
		},
		run: func(*cobra.Command, string, []string) error { return wantErr },
	}
	command := newCrosswalkCmd(runtime)
	command.SetArgs([]string{"profile", "create", "repository-items", "--bundle", "islandora_object"})
	if err := command.Execute(); !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want %v", err, wantErr)
	}
	if !cleaned {
		t.Fatal("config snapshot cleanup was not called after failure")
	}
}

func TestCrosswalkProfileCreateValidatesInstitutionPolicyBeforeExport(t *testing.T) {
	t.Parallel()

	exported := false
	runtime := crosswalkRuntime{
		context: func(string) (*config.Context, error) { return &config.Context{Name: "repository"}, nil },
		exportConfig: func(*cobra.Command, *config.Context) (string, func(), error) {
			exported = true
			return "", func() {}, nil
		},
		run: func(*cobra.Command, string, []string) error { return nil },
	}
	command := newCrosswalkCmd(runtime)
	command.SetArgs([]string{
		"profile", "create", "repository-items", "--bundle", "islandora_object",
		"--institution-scheme", "lehigh-id",
	})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "must be supplied together") {
		t.Fatalf("Execute() error = %v", err)
	}
	if exported {
		t.Fatal("invalid identity policy unexpectedly exported Drupal config")
	}
}

func TestCrosswalkProfileCreateRejectsBlankOutputBeforeExport(t *testing.T) {
	t.Parallel()

	exported := false
	runtime := crosswalkRuntime{
		context: func(string) (*config.Context, error) { return &config.Context{Name: "repository"}, nil },
		exportConfig: func(*cobra.Command, *config.Context) (string, func(), error) {
			exported = true
			return "", func() {}, nil
		},
		run: func(*cobra.Command, string, []string) error { return nil },
	}
	command := newCrosswalkCmd(runtime)
	command.SetArgs([]string{
		"profile", "create", "repository-items", "--bundle", "islandora_object", "--output=",
	})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--output is required") {
		t.Fatalf("Execute() error = %v", err)
	}
	if exported {
		t.Fatal("blank output unexpectedly exported Drupal config")
	}
}

func TestCrosswalkServeResolvesSiteEndpointAndForwardsOptions(t *testing.T) {
	t.Parallel()

	var gotContext string
	var gotArguments []string
	runtime := crosswalkRuntime{
		context: func(name string) (*config.Context, error) {
			gotContext = name
			return &config.Context{Name: "production"}, nil
		},
		resolveJSONAPI: func(_ *cobra.Command, ctx *config.Context) (endpoint.Resolved, error) {
			if ctx.Name != "production" {
				t.Fatalf("endpoint context = %+v", ctx)
			}
			return endpoint.Resolved{URL: "https://repository.example.edu/jsonapi"}, nil
		},
		run: func(_ *cobra.Command, operation string, arguments []string) error {
			if operation != "server" {
				t.Fatalf("operation = %q", operation)
			}
			gotArguments = append([]string(nil), arguments...)
			return nil
		},
	}
	command := newCrosswalkCmd(runtime)
	command.SetArgs([]string{
		"serve", "--context", "production", "--drupal-profile", "repository-items",
		"--address=127.0.0.1:9090", "--drupal-token-env", "REPOSITORY_TOKEN",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if gotContext != "production" {
		t.Fatalf("context = %q, want production", gotContext)
	}
	wantArguments := []string{
		"serve", "--drupal-profile", "repository-items", "--address=127.0.0.1:9090",
		"--drupal-token-env", "REPOSITORY_TOKEN",
		"--drupal-jsonapi", "https://repository.example.edu/jsonapi",
	}
	if !reflect.DeepEqual(gotArguments, wantArguments) {
		t.Fatalf("Crosswalk arguments = %#v, want %#v", gotArguments, wantArguments)
	}
	for _, argument := range gotArguments {
		if argument == "production" {
			t.Fatalf("context name leaked into Crosswalk arguments: %#v", gotArguments)
		}
	}
}

func TestCrosswalkServeFailsClosedForProfileAndEndpointOverrides(t *testing.T) {
	t.Parallel()

	runtime := crosswalkRuntime{
		context: func(string) (*config.Context, error) { return &config.Context{Name: "repository"}, nil },
		resolveJSONAPI: func(*cobra.Command, *config.Context) (endpoint.Resolved, error) {
			return endpoint.Resolved{URL: "https://repository.example.edu/jsonapi"}, nil
		},
		run: func(*cobra.Command, string, []string) error { return nil },
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing profile", args: []string{"serve"}, want: "--drupal-profile is required"},
		{name: "blank profile", args: []string{"serve", "--drupal-profile="}, want: "--drupal-profile is required"},
		{name: "missing profile value", args: []string{"serve", "--drupal-profile", "--address=:9090"}, want: "--drupal-profile is required"},
		{name: "managed endpoint", args: []string{"serve", "--drupal-profile", "items", "--drupal-jsonapi", "https://other.example/jsonapi"}, want: "managed by sitectl"},
		{name: "blank context", args: []string{"serve", "--context=", "--drupal-profile", "items"}, want: "--context requires a value"},
		{name: "missing context value", args: []string{"serve", "--context", "--drupal-profile", "items"}, want: "--context requires a value"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := newCrosswalkCmd(runtime)
			command.SetArgs(test.args)
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRunCrosswalkDoesNotPutCredentialsInArguments(t *testing.T) {
	directory := t.TempDir()
	executable := directory + "/crosswalk-fixture"
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\"\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatalf("write fixture executable: %v", err)
	}
	t.Setenv(crosswalkBinaryEnvironment, executable)
	t.Setenv("DRUPAL_JSONAPI_TOKEN", "top-secret-token")
	command := &cobra.Command{}
	command.SetContext(context.Background())
	var output strings.Builder
	command.SetOut(&output)
	command.SetErr(&output)
	if err := runCrosswalk(command, "test", []string{"serve", "--drupal-token-env", "DRUPAL_JSONAPI_TOKEN"}); err != nil {
		t.Fatalf("runCrosswalk() error = %v", err)
	}
	if strings.Contains(output.String(), "top-secret-token") {
		t.Fatalf("child arguments leaked credential: %q", output.String())
	}
	if !strings.Contains(output.String(), "DRUPAL_JSONAPI_TOKEN") {
		t.Fatalf("child arguments omitted credential reference: %q", output.String())
	}
}
