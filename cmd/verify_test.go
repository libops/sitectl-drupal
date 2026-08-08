package cmd

import (
	"context"
	"errors"
	"reflect"
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
	return r.outputs[key], r.errors[key]
}

func TestRunDrupalVerifyChecksExecutesStrictApplicationAssertions(t *testing.T) {
	runtime := &fakeDrupalVerifyRuntime{outputs: map[string]string{
		"status":                   "Successful\n",
		"sql:query":                "drupal@%\n",
		"config:status":            "[]\n",
		"php:eval":                 `{"cron":true,"queue_workers":4}`,
		"search-api:server-status": `{"status":"available"}`,
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
	if len(runtime.calls) != 5 {
		t.Fatalf("got %d command calls, want 5", len(runtime.calls))
	}
	if got := runtime.calls[3]; !reflect.DeepEqual(got, []string{drushExecutable, "php:eval", drupalQueueProbe}) {
		t.Fatalf("unexpected queue probe: %#v", got)
	}
}

func TestRunDrupalVerifyChecksReportsEveryFailedAssertion(t *testing.T) {
	runtime := &fakeDrupalVerifyRuntime{outputs: map[string]string{
		"status":        "Not bootstrapped",
		"sql:query":     "root@localhost",
		"config:status": `{"system.site":"Different"}`,
		"php:eval":      `{"cron":false,"queue_workers":0}`,
	}, errors: map[string]error{
		"search-api:server-status": errors.New("Solr unavailable"),
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
}

func TestVerifyDrupalConfigDriftRejectsMissingOrInvalidJSON(t *testing.T) {
	for _, output := range []string{"", "not-json", "null", `[{"name":"system.site"}]`} {
		if result := verifyDrupalConfigDrift(output); result.Status != sitevalidate.StatusFailed {
			t.Fatalf("output %q unexpectedly passed: %#v", output, result)
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
	for _, output := range []string{"", `{}`, `[]`, `false`, `not-json`} {
		if result := verifyDrupalSolr(output); result.Status != sitevalidate.StatusFailed {
			t.Fatalf("Solr status %q unexpectedly passed: %#v", output, result)
		}
	}
	for _, output := range []string{`{"status":true}`, `{"default_solr_server":{"status":"available"}}`, `[{"id":"default_solr_server"}]`} {
		if result := verifyDrupalSolr(output); result.Status != sitevalidate.StatusOK {
			t.Fatalf("Solr status %q unexpectedly failed: %#v", output, result)
		}
	}
}
