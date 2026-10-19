package cli

import (
	"strings"
	"testing"
)

func TestRawAPIRejectsUnsafePathsAndPosts(t *testing.T) {
	_, errOut := runCLI(t, "api", "get", "/not-api")
	if !strings.Contains(errOut, "raw API paths must start with /api/") {
		t.Fatalf("stderr = %s", errOut)
	}

	_, errOut = runCLI(t, "api", "post", "/api/projects/123/feature_flags/")
	if !strings.Contains(errOut, "raw POST outside query endpoints requires --yes") {
		t.Fatalf("stderr = %s", errOut)
	}
}

func TestRawAPIPrintRequestRedactsAuth(t *testing.T) {
	out := runOK(t, "api", "get", "/api/organizations/", "--print-request")
	if strings.Contains(out, "phx_mock") || !strings.Contains(out, "Bearer phx_...") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestRawPostOnlySkipsYesForRealQueryEndpoints(t *testing.T) {
	for path, query := range map[string]bool{
		"/api/environments/456/query/":              true,
		"/api/projects/123/query/":                  true,
		"/api/projects/123/feature_flags/":          false,
		"/api/projects/123/feature_flags/query/":    false,
		"/api/projects/123/query/../feature_flags/": false,
		"/api/organizations/org_1/query/":           false,
	} {
		if got := isQueryPath(path); got != query {
			t.Fatalf("isQueryPath(%s) = %v", path, got)
		}
	}
	s := newMockSession(t, "phx_mock", "456")
	if errOut := s.mustFail("api", "post", "/api/projects/123/query/../feature_flags/", "--json", `{"key":"sneaky"}`); !strings.Contains(errOut, ". or .. segments") {
		t.Fatalf("a dot-segment path must be refused before it is sent: %s", errOut)
	}
}
