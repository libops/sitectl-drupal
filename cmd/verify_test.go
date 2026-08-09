package cmd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	sitevalidate "github.com/libops/sitectl/pkg/validate"
)

type fakeDrupalVerifyRuntime struct {
	outputs map[string]string
	errors  map[string]error
	calls   [][]string
}

func (r *fakeDrupalVerifyRuntime) ExecCapture(_ context.Context, _, _ string, argv []string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	key := argv[1]
	if argv[0] == "test" {
		key = "test:" + argv[2]
	} else if key == "php:script" && len(argv) > 2 {
		key = argv[2]
	}
	return r.outputs[key], r.errors[key]
}

func TestRunDrupalVerifyChecksExecutesStrictApplicationAssertions(t *testing.T) {
	runtime := &fakeDrupalVerifyRuntime{outputs: map[string]string{
		"status":                    "Successful\n",
		"sql:query":                 "drupal@%\n",
		"config:status":             "[]\n",
		drupalVerifyCronQueueTarget: `{"cron":true,"queue_workers":4}`,
		drupalVerifySolrTarget:      `{"exists":true,"enabled":true,"available":true}`,
	}, errors: map[string]error{}}

	results := runDrupalVerifyChecks(context.Background(), runtime, "site-drupal-1", "/var/www/drupal")
	if len(results) != 5 {
		t.Fatalf("got %d results, want five real assertions: %#v", len(results), results)
	}
	for _, result := range results {
		if result.Status != sitevalidate.StatusOK {
			t.Fatalf("result %q was not ok: %#v", result.Name, result)
		}
	}
	if len(runtime.calls) != len(drupalTemplatePrograms)+5 {
		t.Fatalf("got %d command calls, want %d", len(runtime.calls), len(drupalTemplatePrograms)+5)
	}
	if got := runtime.calls[len(drupalTemplatePrograms)+3]; !reflect.DeepEqual(got, []string{drushExecutable, "php:script", drupalVerifyCronQueueTarget}) {
		t.Fatalf("unexpected queue probe: %#v", got)
	}
	if got := runtime.calls[len(drupalTemplatePrograms)+4]; !reflect.DeepEqual(got, []string{drushExecutable, "php:script", drupalVerifySolrTarget}) {
		t.Fatalf("unexpected Solr probe: %#v", got)
	}
}

func TestRunDrupalVerifyChecksReportsEveryFailedAssertion(t *testing.T) {
	runtime := &fakeDrupalVerifyRuntime{outputs: map[string]string{
		"status":                      "Not bootstrapped",
		"sql:query":                   "root@localhost",
		"config:status":               `{"system.site":"Different"}`,
		drupalVerifyConfigDriftTarget: `{"system.site":["name","uuid"]}`,
		drupalVerifyCronQueueTarget:   `{"cron":false,"queue_workers":0}`,
	}, errors: map[string]error{
		drupalVerifySolrTarget: errors.New("Solr unavailable"),
	}}

	results := runDrupalVerifyChecks(context.Background(), runtime, "site-drupal-1", "/var/www/drupal")
	if len(results) != 5 {
		t.Fatalf("got %d results, want 5", len(results))
	}
	for _, result := range results {
		if result.Status != sitevalidate.StatusFailed || result.FixHint == "" {
			t.Fatalf("expected actionable failed result, got %#v", result)
		}
	}
	if !strings.Contains(results[2].Detail, "system.site [name, uuid]") {
		t.Fatalf("config drift did not report bounded field evidence: %#v", results[2])
	}
}

func TestRunDrupalVerifyChecksFailsClearlyForOlderTemplate(t *testing.T) {
	runtime := &fakeDrupalVerifyRuntime{
		outputs: map[string]string{},
		errors: map[string]error{
			"test:" + drupalVerifyConfigDriftTarget: errors.New("missing"),
		},
	}

	results := runDrupalVerifyChecks(context.Background(), runtime, "site-drupal-1", "/var/www/drupal")
	if len(results) != 1 || results[0].Name != "verify:drupal:template-programs" || results[0].Status != sitevalidate.StatusFailed {
		t.Fatalf("unexpected compatibility result: %#v", results)
	}
	for _, want := range []string{drupalVerifyConfigDriftTarget, drupalCreateRepo, drupalTemplateVersion} {
		if !strings.Contains(results[0].Detail+" "+results[0].FixHint, want) {
			t.Fatalf("compatibility result omitted %q: %#v", want, results[0])
		}
	}
	if len(runtime.calls) != 3 {
		t.Fatalf("verification continued after missing program: %#v", runtime.calls)
	}
}

func TestVerifyDrupalConfigDriftRejectsMissingOrInvalidJSON(t *testing.T) {
	for _, output := range []string{"", "not-json", "null", `[{"name":"system.site"}]`} {
		if result := verifyDrupalConfigDrift(output); result.Status != sitevalidate.StatusFailed {
			t.Fatalf("output %q unexpectedly passed: %#v", output, result)
		}
	}
}

func TestVerifyDrupalConfigDriftReportsConfigurationNamesAndStates(t *testing.T) {
	result := verifyDrupalConfigDrift(`{"system.site":{"name":"system.site","state":"Different"},"search_api.server.default_solr_server":{"name":"search_api.server.default_solr_server","state":"Only in DB"}}`)
	if result.Status != sitevalidate.StatusFailed {
		t.Fatalf("drift unexpectedly passed: %#v", result)
	}
	for _, expected := range []string{"search_api.server.default_solr_server (Only in DB)", "system.site (Different)"} {
		if !strings.Contains(result.Detail, expected) {
			t.Fatalf("drift detail %q does not contain %q", result.Detail, expected)
		}
	}
}

func TestVerifyDrupalCronQueueAllowsNoConfiguredWorkers(t *testing.T) {
	if result := verifyDrupalCronQueue(`{"cron":true,"queue_workers":0}`); result.Status != sitevalidate.StatusOK {
		t.Fatalf("zero configured workers should be ready: %#v", result)
	}
	if result := verifyDrupalCronQueue(`{"cron":true}`); result.Status != sitevalidate.StatusFailed {
		t.Fatalf("missing queue discovery result unexpectedly passed: %#v", result)
	}
}

func TestVerifyDrupalSolrRequiresStructuredCommandEvidence(t *testing.T) {
	for _, output := range []string{"", `{}`, `false`, `not-json`, `{"exists":true,"enabled":false,"available":true}`, `{"exists":true,"enabled":true,"available":false}`} {
		if result := verifyDrupalSolr(output); result.Status != sitevalidate.StatusFailed {
			t.Fatalf("Solr status %q unexpectedly passed: %#v", output, result)
		}
	}
	output := `{"exists":true,"enabled":true,"available":true}`
	if result := verifyDrupalSolr(output); result.Status != sitevalidate.StatusOK {
		t.Fatalf("Solr status %q unexpectedly failed: %#v", output, result)
	}
}
