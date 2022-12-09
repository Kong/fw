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

// parseServerUris parses the server uri's after rendering the template variables.
// result will always have at least 1 entry, but not necessarily a hostname/port/scheme
func parseServerUris(servers *openapi3.Servers) ([]*url.URL, error) {
	var targets []*url.URL

	if servers == nil || len(*servers) == 0 {
		uriObject, _ := url.ParseRequestURI("/") // path '/' is the default for empty server blocks
		targets = make([]*url.URL, 1)
		targets[0] = uriObject

	} else {
		targets = make([]*url.URL, len(*servers))

		for i, server := range *servers {
			uriString := server.URL
			for name, svar := range server.Variables {
				uriString = strings.ReplaceAll(uriString, "{"+name+"}", svar.Default)
			}

			uriObject, err := url.ParseRequestURI(uriString)
			if err != nil {
				return targets, fmt.Errorf("failed to parse uri '%s'; %w", uriString, err)
			}

			targets[i] = uriObject
		}
	}

	return targets, nil
}

// setServerDefaults sets the scheme and port if missing and inferable.
// It's set based on; scheme given, port (80/443), default-scheme. In that order.
func setServerDefaults(targets []*url.URL, schemeDefault string) {
	for _, target := range targets {
		// set the hostname if unset
		if target.Host == "" {
			target.Host = "localhost"
		}

		// set the scheme if unset
		if target.Scheme == "" {
			// detect scheme from the port
			switch target.Port() {
			case "80":
				target.Scheme = "http"

			case "443":
				target.Scheme = "https"

			default:
				target.Scheme = schemeDefault
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

// createKongUpstream create a new upstream entity.
func createKongUpstream(
	baseName string, // slugified name of the upstream, and uuid input
	servers *openapi3.Servers, // the OAS3 server block to use for generation
	upstreamDefaults string, // defaults to use (JSON string) or empty if no defaults
	tags []string, // tags to attach to the new upstream
	uuidNamespace uuid.UUID) (map[string]interface{}, error) {

	var upstream map[string]interface{}

	// have to create an upstream with targets
	if upstreamDefaults != "" {
		// got defaults, so apply them
		json.Unmarshal([]byte(upstreamDefaults), &upstream)
	} else {
		upstream = make(map[string]interface{})
	}

	upstreamName := baseName + ".upstream"
	upstream["id"] = uuid.NewV5(uuidNamespace, upstreamName).String()
	upstream["name"] = upstreamName
	upstream["tags"] = tags

	// the server urls, will have minimum 1 entry on success
	targets, err := parseServerUris(servers)
	if err != nil {
		return nil, fmt.Errorf("failed to generate upstream: %w", err)
	}

	setServerDefaults(targets, "https")

	// now add the targets to the upstream
	upstreamTargets := make([]map[string]interface{}, len(targets))
	for i, target := range targets {
		t := make(map[string]interface{})
		t["target"] = target.Host
		t["tags"] = tags
		upstreamTargets[i] = t
	}
	upstream["targets"] = upstreamTargets

	return upstream, nil
}

// CreateKongService creates a new Kong service entity, and optional upstream.
// `baseName` will be used as the name of the service (slugified), and as input
// for the UUIDv5 generation.
func CreateKongService(
	baseName string, // slugified name of the service, and uuid input
	servers *openapi3.Servers,
	serviceDefaults string,
	upstreamDefaults string,
	tags []string,
	uuidNamespace uuid.UUID) (map[string]interface{}, map[string]interface{}, error) {

	var (
		service  map[string]interface{}
		upstream map[string]interface{}
	)

	// setup the defaults
	if serviceDefaults != "" {
		json.Unmarshal([]byte(serviceDefaults), &service)
	} else {
		service = make(map[string]interface{})
	}

	// add id, name and tags to the service
	service["id"] = uuid.NewV5(uuidNamespace, baseName+".service").String()
	service["name"] = baseName
	service["tags"] = tags
	service["plugins"] = make([]interface{}, 0)
	service["routes"] = make([]interface{}, 0)

	// the server urls, will have minimum 1 entry on success
	targets, err := parseServerUris(servers)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create service: %w", err)
	}

	// fill in the scheme of the url if missing. Use service-defaults for the default scheme
	defaultScheme := "https"
	if service["protocol"] != nil {
		defaultScheme = service["protocol"].(string)
	}
	setServerDefaults(targets, defaultScheme)

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
	if len(targets) == 1 && upstreamDefaults == "" {
		// have to create a simple service, no upstream, so just set the hostname
		service["host"] = targets[0].Hostname()
	} else {
		// have to create an upstream with targets
		upstream, err = createKongUpstream(baseName, servers, upstreamDefaults, tags, uuidNamespace)
		if err != nil {
			return nil, nil, err
		}
		service["host"] = upstream["name"]
	}

	return service, upstream, nil
}
