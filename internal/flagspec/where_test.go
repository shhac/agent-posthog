package flagspec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseWhere(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"email icontains @example.com", `{"key":"email","operator":"icontains","type":"person","value":"@example.com"}`},
		{"app_env = staging", `{"key":"app_env","operator":"exact","type":"person","value":"staging"}`},
		{`email = ["a@x.com","b@x.com"]`, `{"key":"email","operator":"exact","type":"person","value":["a@x.com","b@x.com"]}`},
		{"plan != free", `{"key":"plan","operator":"is_not","type":"person","value":"free"}`},
		{"age >= 21", `{"key":"age","operator":"gte","type":"person","value":21}`},
		{"is_beta = true", `{"key":"is_beta","operator":"exact","type":"person","value":true}`},
		{`is_beta = "true"`, `{"key":"is_beta","operator":"exact","type":"person","value":"true"}`},
		{"name = Ada Lovelace", `{"key":"name","operator":"exact","type":"person","value":"Ada Lovelace"}`},
		{"email is_set", `{"key":"email","operator":"is_set","type":"person","value":"is_set"}`},
		{"person:cohort = x", `{"key":"cohort","operator":"exact","type":"person","value":"x"}`},
		{"cohort in 12", `{"key":"id","operator":"in","type":"cohort","value":12}`},
		{"cohort not_in 12", `{"key":"id","operator":"not_in","type":"cohort","value":12}`},
		{"flag:checkout-v2 = true", `{"key":"checkout-v2","operator":"flag_evaluates_to","type":"flag","value":true}`},
		{"flag:22 = treatment", `{"key":"22","operator":"flag_evaluates_to","type":"flag","value":"treatment"}`},
		{"zip icontains 90210", `{"key":"zip","operator":"icontains","type":"person","value":"90210"}`},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			got, err := ParseWhere(tc.expr)
			if err != nil {
				t.Fatalf("ParseWhere: %v", err)
			}
			data, _ := json.Marshal(got)
			if string(data) != tc.want {
				t.Fatalf("got  %s\nwant %s", data, tc.want)
			}
		})
	}
}

func TestParseWhereRejects(t *testing.T) {
	cases := map[string]string{
		"email":                     "expected",
		"email frobs x":             "unknown operator",
		"email =":                   "missing value",
		"email is_set yes":          "takes no value",
		"age > old":                 "needs a number",
		"cohort = 12":               "only 'in' and 'not_in'",
		"cohort in abc":             "numeric cohort ID",
		"flag:x icontains y":        "only '='",
		"flag:x = 5":                "true, false, or a variant",
		"email = []":                "empty list",
		`email = [{"a":1}]`:         "list items",
		`email icontains ["a","b"]`: "needs a string",
		`email = {"nested": "obj"}`: "value must be",
		"flag: = true":              "missing flag key",
	}
	for expr, want := range cases {
		t.Run(expr, func(t *testing.T) {
			_, err := ParseWhere(expr)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want containing %q", err, want)
			}
		})
	}
}

func TestFormatWhereRoundTrips(t *testing.T) {
	for _, expr := range []string{
		"email icontains @example.com",
		`email = ["a@x.com","b@x.com"]`,
		"age >= 21",
		"email is_set",
		"cohort in 12",
		"flag:22 = true",
		"person:cohort = x",
		`is_beta = "true"`,
		"name = Ada Lovelace",
	} {
		prop, err := ParseWhere(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		got, ok := FormatWhere(prop)
		if !ok || got != expr {
			t.Fatalf("FormatWhere(%s) = %q, %v", expr, got, ok)
		}
	}
}

func TestFormatWhereRefusesLossyProperties(t *testing.T) {
	for _, raw := range []string{
		`{"key":"email","operator":"exact","type":"person","value":"a","negation":false}`,
		`{"key":"email","type":"person","value":"a"}`,
		`{"key":"$browser","operator":"exact","type":"event","value":"Chrome"}`,
		`{"key":"name","operator":"exact","type":"group","value":"Acme","group_type_index":0}`,
	} {
		prop, _ := DecodeObject([]byte(raw))
		if expr, ok := FormatWhere(prop); ok {
			t.Fatalf("FormatWhere(%s) = %q, want refusal", raw, expr)
		}
	}
}
