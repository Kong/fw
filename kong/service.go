package kong

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	uuid "github.com/satori/go.uuid"
)

// parses the server uri's after rendering the template variables.
// result will always have at least 1 entry, but not necessarily a hostname/port/scheme
func parseServerUris(servers *openapi3.Servers) ([]*url.URL, error) {
	var targets []*url.URL

	if servers == nil || len(*servers) == 0 {
		uri_obj, _ := url.ParseRequestURI("/") // path '/' is the default for empty server blocks
		targets = make([]*url.URL, 1)
		targets[0] = uri_obj

	} else {
		targets = make([]*url.URL, len(*servers))

		for i, server := range *servers {
			uri_string := server.URL
			for name, svar := range server.Variables {
				uri_string = strings.ReplaceAll(uri_string, "{"+name+"}", svar.Default)
			}

			uri_obj, err := url.ParseRequestURI(uri_string)
			if err != nil {
				return targets, fmt.Errorf("failed to parse uri '%s'; %w", uri_string, err)
			}

			targets[i] = uri_obj
		}
	}

	return targets, nil
}

// sets the scheme and port if missing.
// It's set based on; scheme given, port (80/443), default_scheme. In that order.
func setServerDefaults(targets []*url.URL, scheme_default string) {
	for _, target := range targets {
		// set the scheme if unset
		if target.Scheme == "" {
			// detect scheme from the port
			switch target.Port() {
			case "80":
				target.Scheme = "http"

			case "443":
				target.Scheme = "https"

			default:
				target.Scheme = scheme_default
			}
		}

		// set the port if unset (but a host is given)
		if target.Host != "" && target.Port() == "" {
			if target.Scheme == "http" {
				target.Host = target.Host + ":80"
			}
			if target.Scheme == "https" {
				target.Host = target.Host + ":443"
			}
		}
	}
}

// Create a new upstream
func createKongUpstream(base_name string, // name of the service (will be slugified), and uuid input
	servers *openapi3.Servers, // the OAS3 server block to use for generation
	upstream_defaults string, // defaults to use (JSON string) or empty if no defaults
	tags []string, // tags to attach to the new upstream
	uuid_namespace uuid.UUID) (map[string]interface{}, error) {

	var upstream map[string]interface{}

	// have to create an upstream with targets
	if upstream_defaults != "" {
		// got defaults, so apply them
		json.Unmarshal([]byte(upstream_defaults), &upstream)
	}
	upstream["id"] = uuid.NewV5(uuid_namespace, base_name+".upstream").String()
	upstream["name"] = Slugify(base_name) + ".upstream"
	upstream["tags"] = tags

	// the server urls, will have minimum 1 entry on success
	targets, err := parseServerUris(servers)
	if err != nil {
		return nil, fmt.Errorf("failed to generate upstream: %w", err)
	}

	setServerDefaults(targets, "https")

	// now add the targets to the upstream
	upstream_targets := make([]map[string]interface{}, len(targets))
	for i, target := range targets {
		t := make(map[string]interface{})
		t["target"] = target.Host
		t["tags"] = tags
		upstream_targets[i] = t
	}
	upstream["targets"] = upstream_targets

	return upstream, nil
}

// Creates a new Kong service entity, and optional upstream.
// `base_name` will be used as the name of the service (slugified), and as input
// for the UUIDv5 generation.
func CreateKongService(
	base_name string, // name of the service (will be slugified), and uuid input
	servers *openapi3.Servers,
	service_defaults string,
	upstream_defaults string,
	tags []string,
	uuid_namespace uuid.UUID) (map[string]interface{}, map[string]interface{}, error) {

	var service map[string]interface{}
	var upstream map[string]interface{}

	// setup the defaults
	json.Unmarshal([]byte(service_defaults), &service)

	// add id, name and tags to the service
	service["id"] = uuid.NewV5(uuid_namespace, base_name+".service").String()
	service["name"] = Slugify(base_name)
	service["tags"] = tags
	service["routes"] = make([]interface{}, 0)

	// the server urls, will have minimum 1 entry on success
	targets, err := parseServerUris(servers)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create service: %w", err)
	}

	// fill in the scheme of the url if missing. Use service-defaults for the default scheme
	default_scheme := "https"
	if service["protocol"] != nil {
		default_scheme = service["protocol"].(string)
	}
	setServerDefaults(targets, default_scheme)

	service["protocol"] = targets[0].Scheme
	service["path"] = targets[0].Path
	if targets[0].Port() != "" {
		// port is provided, so parse it
		service["port"], _ = strconv.ParseInt(targets[0].Port(), 10, 16)
	} else {
		// no port provided, so set it based on scheme, where https/443 is the default
		if targets[0].Scheme != "http" {
			service["port"] = 443
		} else {
			service["port"] = 80
		}
	}

	// we need an upstream if;
	// a) upstream defaults are provided, or
	// b) there is more than one entry in the servers block
	if len(targets) == 1 && upstream_defaults == "" {
		// have to create a simple service, no upstream, so just set the hostname
		service["host"] = targets[0].Hostname()
	} else {
		// have to create an upstream with targets
		upstream, err := createKongUpstream(base_name, servers, upstream_defaults, tags, uuid_namespace)
		if err != nil {
			return nil, nil, err
		}
		service["host"] = upstream["name"]
	}

	return service, upstream, nil
}
