package cli

import (
	"strings"
	"testing"
)

func TestV2FlagsRoundTripAsWholeDocuments(t *testing.T) {
	s := flagSession(t)
	out, errOut, err := s.run("flags", "export", "v2-flag", "--format", "yaml")
	if err != nil || !strings.Contains(out, "filters_document:") || strings.Contains(out, "conditions:") {
		t.Fatalf("export err=%v stderr=%s\n%s", err, errOut, out)
	}
	if r := s.mustRun("flags", "apply", "--file", writeTemp(t, "v2.yaml", out), "--yes"); r["outcome"] != "unchanged" {
		t.Fatalf("export → apply changed the flag: %v", r)
	}

	edited := strings.Replace(out, "@example.com", "@example.org", 1)
	r := s.mustRun("flags", "apply", "--file", writeTemp(t, "edited.yaml", edited), "--yes")
	if r["outcome"] != "applied" || !strings.Contains(mustJSON(r["changes"]), "filters.rules[0].when") {
		t.Fatalf("apply = %v", r)
	}
	if doc := mustJSON(s.flag("v2-flag")["filters"]); !strings.Contains(doc, "@example.org") || !strings.Contains(doc, `"version":2`) {
		t.Fatalf("filters = %s", doc)
	}
	if r := s.mustRun("flags", "enable", "v2-flag", "--yes"); r["outcome"] != "applied" {
		t.Fatalf("enable = %v", r)
	}
}

func TestV2FlagsRefuseNarrowEditsAndMixedSpecs(t *testing.T) {
	s := flagSession(t)
	for _, args := range [][]string{
		{"flags", "conditions", "add", "v2-flag", "--where", "plan = pro"},
		{"flags", "conditions", "set", "v2-flag", "0", "--rollout", "5", "--if-version", "3"},
		{"flags", "payload", "set", "v2-flag", "--json", "1"},
		{"flags", "apply", "--spec-json", `{"key":"v2-flag","conditions":[]}`},
	} {
		if errOut := s.mustFail(args...); !strings.Contains(errOut, "filters_document") {
			t.Fatalf("%v: stderr = %s", args, errOut)
		}
	}
	for spec, want := range map[string]string{
		`{"key":"v2-flag","filters_document":{"version":2},"conditions":[]}`: "drop conditions",
		`{"key":"v2-flag","filters_document":{"groups":[]}}`:                 "version: 2",
		`{"key":"checkout-v2","filters_document":{"version":2}}`:             "classic filters",
		`{"key":"new-v2","active":true,"filters_document":{"version":2}}`:    "flags disabled",
	} {
		if errOut := s.mustFail("flags", "apply", "--spec-json", spec); !strings.Contains(errOut, want) {
			t.Fatalf("%s: stderr = %s", spec, errOut)
		}
	}
	if r := s.mustRun("flags", "apply", "--spec-json", `{"key":"new-v2","filters_document":{"version":2,"rules":[]}}`, "--yes"); r["outcome"] != "applied" {
		t.Fatalf("v2 create = %v", r)
	}
}
