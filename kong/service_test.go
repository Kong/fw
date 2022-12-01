package kong

import (
	"net/url"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/go-cmp/cmp"
)

func Test_parseServerUris(t *testing.T) {

	// basics

	servers := &openapi3.Servers{
		{
			URL: "http://cookiemonster.com/chocolate/cookie",
		}, {
			URL: "https://konghq.com/bitter/sweet",
		},
	}
	expected := []url.URL{
		{
			Scheme: "https",
			Host:   "konghq.com",
			Path:   "/bitter/sweet",
		},
	}
	targets := parseServerUris(servers)
	if diff := cmp.Diff(targets, expected); diff != "" {
		t.Errorf(diff)
	}

	// replaces variables with defaults

	servers = &openapi3.Servers{
		{
			URL: "http://{var1}-{var2}.com/chocolate/cookie",
			Variables: map[string]*openapi3.ServerVariable{
				"var1": {
					Default: "hello",
					Enum:    []string{"hello", "world"},
				},
				"var2": {
					Default: "Welt",
					Enum:    []string{"hallo", "Welt"},
				},
			},
		},
	}
	expected = []url.URL{
		{
			Scheme: "http",
			Host:   "hello-Welt.com",
			Path:   "/chocolate/cookie",
		},
	}
	targets = parseServerUris(servers)
	if diff := cmp.Diff(targets, expected); diff != "" {
		t.Errorf(diff)
	}

	// returns error on a bad URL
	// TODO: requires signature change

	// returns error if servers is empty

}
