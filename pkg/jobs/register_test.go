package jobs

import (
	"bytes"
	"strings"
	"testing"
)

func TestMaximumBytesWriterEnforcesLimit(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	writer := &maximumBytesWriter{writer: &output, remaining: 5, maximum: 5}
	if count, err := writer.Write([]byte("abc")); err != nil || count != 3 {
		t.Fatalf("first Write() = %d, %v", count, err)
	}
	count, err := writer.Write([]byte("def"))
	if count != 2 || err == nil || !strings.Contains(err.Error(), "exceeds 5 compressed bytes") {
		t.Fatalf("overflow Write() = %d, %v", count, err)
	}
	if output.String() != "abcde" {
		t.Fatalf("output = %q, want abcde", output.String())
	}
	if count, err := writer.Write([]byte("z")); count != 0 || err == nil {
		t.Fatalf("post-limit Write() = %d, %v", count, err)
	}
}

func TestCrosswalkConfigSnapshotSelectsOnlyModelInputs(t *testing.T) {
	t.Parallel()

	for _, required := range []string{
		"system.site.yml",
		"field.storage.*.yml",
		"field.field.*.yml",
		"rdf.mapping.*.yml",
		`[ -f "$file" ]`,
		`tar -czf - "$@"`,
	} {
		if !strings.Contains(crosswalkConfigSnapshotScript, required) {
			t.Fatalf("snapshot command does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"settings.php", ".env", "secrets", "credentials"} {
		if strings.Contains(crosswalkConfigSnapshotScript, forbidden) {
			t.Fatalf("snapshot command unexpectedly contains %q", forbidden)
		}
	}
}
