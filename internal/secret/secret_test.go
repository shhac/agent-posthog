package secret

import (
	"strings"
	"testing"
)

func TestRedactURLMasksKeysAndSecretParameters(t *testing.T) {
	got := RedactURL("https://us.posthog.com/api/x/?token=abc&q=phc_secret123&next=%3Dphx_personal&page=2")
	for _, leaked := range []string{"abc", "secret123", "personal"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("%q leaked in %s", leaked, got)
		}
	}
	if !strings.Contains(got, "page=2") || !strings.Contains(got, "token=...") {
		t.Fatalf("redacted too much: %s", got)
	}
}

func TestFieldsAndKeysAgree(t *testing.T) {
	if !IsField("personal_api_key") || IsField("name") {
		t.Fatal("field names misclassified")
	}
	if !IsKey("phs_share") || IsKey("ph_not_a_key") {
		t.Fatal("key prefixes misclassified")
	}
}
