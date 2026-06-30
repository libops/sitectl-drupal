package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/libops/sitectl/pkg/plugin"
)

func TestProjectDetectClaimsPlainDrupalProject(t *testing.T) {
	projectDir := t.TempDir()
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"), "services:\n  drupal:\n    image: drupal:latest\n")
	writeFileForTest(t, filepath.Join(projectDir, "composer.json"), `{"require": {}}`)

	result := runDrupalProjectDetectForTest(t, projectDir)
	if !result.Claimed {
		t.Fatalf("expected Drupal project detection to claim %s", projectDir)
	}
	if result.Plugin != "drupal" {
		t.Fatalf("expected drupal claim, got %#v", result)
	}
}

func TestProjectDetectDeclinesISLEServices(t *testing.T) {
	projectDir := t.TempDir()
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"), "services:\n  alpaca:\n    image: libops/alpaca:2\n  drupal:\n    image: islandora/drupal:main\n")
	writeFileForTest(t, filepath.Join(projectDir, "composer.json"), `{"require": {}}`)

	result := runDrupalProjectDetectForTest(t, projectDir)
	if result.Claimed {
		t.Fatalf("expected Drupal project detection to decline ISLE services, got %#v", result)
	}
}

func runDrupalProjectDetectForTest(t *testing.T, projectDir string) projectDetectResultForTest {
	t.Helper()

	sdk := plugin.NewSDK(plugin.Metadata{Name: "drupal"})
	if err := RegisterCommands(sdk); err != nil {
		t.Fatalf("RegisterCommands() error = %v", err)
	}

	req, err := plugin.NewProjectDetectRequest(projectDir)
	if err != nil {
		t.Fatalf("NewProjectDetectRequest() error = %v", err)
	}
	requestData, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal(RPCRequest) error = %v", err)
	}

	cmd := sdk.GetRPCCommand()
	var stdout bytes.Buffer
	cmd.SetIn(bytes.NewReader(requestData))
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var resp plugin.RPCResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(RPCResponse) error = %v: %s", err, stdout.String())
	}
	if !resp.OK {
		t.Fatalf("project.detect failed: %+v", resp.Error)
	}
	var result projectDetectResultForTest
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Unmarshal(project detect result) error = %v", err)
	}
	return result
}

type projectDetectResultForTest struct {
	Claimed    bool   `json:"claimed"`
	Plugin     string `json:"plugin"`
	ProjectDir string `json:"project_dir"`
	Reason     string `json:"reason"`
}

func writeFileForTest(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
