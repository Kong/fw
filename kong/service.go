package kong

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	uuid "github.com/satori/go.uuid"
)

// type KongService struct {
// 	Servers  *openapi3.Servers // the OpenAPI servers block this service is created from
// 	Defaults *interface{}      // the defaults from `x-kong-service-defaults`
// }

// parses the server uri's after rendering the template variables
func parseServerUris(servers *openapi3.Servers) []url.URL {
	targets := make([]url.URL, len(*servers))

	for i, server := range *servers {
		uri_string := server.URL
		for name, svar := range server.Variables {
			uri_string = strings.ReplaceAll(uri_string, "{"+name+"}", svar.Default)
		}

		uri_obj, err := url.ParseRequestURI(uri_string)
		if err != nil {
			log.Fatalf(fmt.Sprintf("failed to parse uri '%s'; %%w", uri_string), err)
		}

		targets[i] = *uri_obj
	}
	if len(targets) == 0 {
		log.Fatal("No urls have been defined in 'servers'")
	}
	return targets
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
	uuid_namespace uuid.UUID) (map[string]interface{}, map[string]interface{}) {

	var service map[string]interface{}
	var upstream map[string]interface{}

	// setup the defaults
	json.Unmarshal([]byte(service_defaults), &service)

	// add id, name and tags to the service
	service["id"] = uuid.NewV5(uuid_namespace, base_name+".service").String()
	service["name"] = Slugify(base_name)
	service["tags"] = tags
	service["routes"] = make([]interface{}, 0)

	// the server urls, will have minimum 1 entry
	targets := parseServerUris(servers)

	service["protocol"] = targets[0].Scheme
	service["path"] = targets[0].Path
	if targets[0].Port() != "" {
		service["port"], _ = strconv.ParseInt(targets[0].Port(), 10, 16)
	} else {
		if targets[0].Scheme == "https" {
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
		if upstream_defaults != "" {
			// got defaults, so apply them
			json.Unmarshal([]byte(upstream_defaults), &upstream)
		}
		upstream["id"] = uuid.NewV5(uuid_namespace, base_name+".upstream").String()
		upstream["name"] = base_name + ".upstream"
		service["host"] = base_name + ".upstream"
		upstream["tags"] = tags

		// now add the targets to the upstream
		upstream_targets := make([]map[string]interface{}, len(targets))
		for i, target := range targets {
			port := target.Port()
			if port == "" {
				if target.Scheme == "https" {
					port = "443"
				} else {
					port = "80"
				}
			}

			t := make(map[string]interface{})
			t["target"] = target.Hostname() + ":" + port
			t["tags"] = tags
			upstream_targets[i] = t
		}
		upstream["targets"] = upstream_targets
	}

	return service, upstream
}
