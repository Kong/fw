package convert

import (
	"encoding/json"
	"fmt"
	"fw/kong"

	"github.com/getkin/kin-openapi/openapi3"
	uuid "github.com/satori/go.uuid"
)

// O2KOptions defines the options for an O2K conversion operation
type O2kOptions struct {
	Tags          []string  // Array of tags to mark all generated entities with
	DocName       string    // Base document name, will be taken from x-kong-name, or info.title (used for UUID generation!)
	UuidNamespace uuid.UUID // Namespace for UUID generation, defaults to DNS namespace for UUID v5
}

// setDefaults sets the defaults for ConvertOas3 operation.
func (opts *O2kOptions) setDefaults() {
	var empty_uuid uuid.UUID
	if uuid.Equal(empty_uuid, opts.UuidNamespace) {
		opts.UuidNamespace = uuid.NamespaceDNS
	}
}

// ConvertOas3 converts an OpenAPI spec to a Kong declarative file.
func ConvertOas3(content *[]byte, opts O2kOptions) (map[string]interface{}, error) {
	opts.setDefaults()

	// set up output document
	result := make(map[string]interface{})
	result["_format_version"] = "3.0"
	services := make([]interface{}, 0)
	upstreams := make([]interface{}, 0)

	var err error
	var doc *openapi3.T // The OAS3 document we're operating on

	var doc_servers *openapi3.Servers // servers block on document level
	var doc_service map[string]interface{}
	var doc_upstream map[string]interface{}

	var path_servers *openapi3.Servers // servers block on current path level
	var path_service map[string]interface{}
	var path_upstream map[string]interface{}

	var operation_servers *openapi3.Servers // servers block on current operation level
	var operation_service map[string]interface{}
	var operation_upstream map[string]interface{}

	// Load and parse the OAS file
	loader := openapi3.NewLoader()
	doc, err = loader.LoadFromData(*content)
	if err != nil {
		return nil, fmt.Errorf("error parsing OAS3 file: [%w]", err)
	}

	// set document level elements
	doc_servers = &doc.Servers // this one is always set, but can be empty

	// determine document name, precedence: specified -> x-kong-name -> Info.Title
	if opts.DocName == "" {
		if doc.ExtensionProps.Extensions["x-kong-name"] != nil {
			err = json.Unmarshal(doc.ExtensionProps.Extensions["x-kong-name"].(json.RawMessage), &opts.DocName)
			if err != nil {
				return nil, fmt.Errorf("expected 'x-kong-name' to be a string; %w", err)
			}
		} else {
			opts.DocName = doc.Info.Title
		}
	}

	// for defaults we keep strings, so deserializing them provides a copy right away
	var doc_service_defaults string // string representation of service-defaults on document level
	if doc.ExtensionProps.Extensions["x-kong-service-defaults"] != nil {
		jsonblob, _ := json.Marshal(doc.ExtensionProps.Extensions["x-kong-service-defaults"])
		doc_service_defaults = string(jsonblob)
	} else {
		doc_service_defaults = "{}" // just empty JSON object
	}

	var doc_upstream_defaults string // string representation of upstream-defaults on document level
	if doc.ExtensionProps.Extensions["x-kong-upstream-defaults"] != nil {
		jsonblob, _ := json.Marshal(doc.ExtensionProps.Extensions["x-kong-upstream-defaults"])
		doc_upstream_defaults = string(jsonblob)
	} else {
		doc_upstream_defaults = ""
	}

	// create the top-level doc_service and (optional) doc_upstream
	doc_service, doc_upstream, err = kong.CreateKongService(opts.DocName, doc_servers, doc_service_defaults, doc_upstream_defaults, opts.Tags, opts.UuidNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create service/updstream from document root: %w", err)
	}
	services = append(services, doc_service)
	if doc_upstream != nil {
		upstreams = append(upstreams, doc_upstream)
	}

	for path, pathitem := range doc.Paths {

		// var path_routes []interface{} // the routes array we need to add to

		// Set up the defaults on the Path level
		new_service := false
		var path_service_defaults string // string representation of service-defaults on path level
		if pathitem.ExtensionProps.Extensions["x-kong-service-defaults"] != nil {
			jsonblob, _ := json.Marshal(pathitem.ExtensionProps.Extensions["x-kong-service-defaults"])
			path_service_defaults = string(jsonblob)
			new_service = true
		} else {
			path_service_defaults = doc_service_defaults
		}

		var path_upstream_defaults string // string representation of upstream-defaults on path level
		if pathitem.ExtensionProps.Extensions["x-kong-upstream-defaults"] != nil {
			jsonblob, _ := json.Marshal(pathitem.ExtensionProps.Extensions["x-kong-upstream-defaults"])
			path_upstream_defaults = string(jsonblob)
			new_service = true
		} else {
			path_upstream_defaults = doc_upstream_defaults
		}

		// if there is no path level servers block, use the document one
		path_servers = &pathitem.Servers
		if len(*path_servers) == 0 { // it's always set, so we ignore it if empty
			path_servers = doc_servers
		} else {
			new_service = true
		}

		// create a new service if we need to do so
		if new_service {
			// create the path-level service and (optional) upstream
			// TODO: the path ends up with / in the hostname of the service
			path_service, path_upstream, err = kong.CreateKongService(
				opts.DocName+"_"+path,
				path_servers,
				path_service_defaults,
				path_upstream_defaults,
				opts.Tags,
				opts.UuidNamespace)
			if err != nil {
				return nil, fmt.Errorf("failed to create service/updstream from path '%s': %w", path, err)
			}
			services = append(services, path_service)
			if path_upstream != nil {
				upstreams = append(upstreams, path_upstream)
			}
			// path_routes = path_service["routes"].([]interface{})
		} else {
			path_service = doc_service
			// path_routes = doc_service["routes"].([]interface{})
		}

		// traverse all operations

		for method, operation := range pathitem.Operations() {

			var operation_routes []interface{} // the routes array we need to add to

			// Set up the defaults on the Operation level
			new_service := false
			var operation_service_defaults string // string representation of service-defaults on operation level
			if operation.ExtensionProps.Extensions["x-kong-service-defaults"] != nil {
				jsonblob, _ := json.Marshal(operation.ExtensionProps.Extensions["x-kong-service-defaults"])
				operation_service_defaults = string(jsonblob)
				new_service = true
			} else {
				operation_service_defaults = path_service_defaults
			}

			var operation_upstream_defaults string // string representation of upstream-defaults on operation level
			if operation.ExtensionProps.Extensions["x-kong-upstream-defaults"] != nil {
				jsonblob, _ := json.Marshal(operation.ExtensionProps.Extensions["x-kong-upstream-defaults"])
				operation_upstream_defaults = string(jsonblob)
				new_service = true
			} else {
				operation_upstream_defaults = path_upstream_defaults
			}

			// if there is no operation level servers block, use the path one
			operation_servers = operation.Servers
			if operation_servers == nil || len(*operation_servers) == 0 {
				operation_servers = path_servers
			} else {
				new_service = true
			}

			// create a new service if we need to do so
			if new_service {
				// create the operation-level service and (optional) upstream
				// TODO: the path ends up with / in the hostname of the service
				operation_service, operation_upstream, err = kong.CreateKongService(
					opts.DocName+"_"+path+"_"+method, //TODO: use operation ID if available
					operation_servers,
					operation_service_defaults,
					operation_upstream_defaults,
					opts.Tags,
					opts.UuidNamespace)
				if err != nil {
					return nil, fmt.Errorf("failed to create service/updstream from operation '%s %s': %w", path, method, err)
				}
				services = append(services, operation_service)
				if operation_upstream != nil {
					upstreams = append(upstreams, operation_upstream)
				}
				operation_routes = operation_service["routes"].([]interface{})
			} else {
				operation_service = path_service
				operation_routes = operation_service["routes"].([]interface{})
			}

			// TODO: add route-defaults on all levels

			// prefix, _ := operation_servers.BasePath()
			// println(method, prefix, path)

			// construct the route
			route := make(map[string]interface{}) // the newly generated Route // TODO: create it from route-defaults
			// TODO: create and add a route-name, using operation id

			// TODO: create and add an ID
			route["paths"] = []string{path} // TODO: convert path to regex before use, or to new router DSL
			route["methods"] = []string{method}
			route["tags"] = opts.Tags

			operation_routes = append(operation_routes, route)
			operation_service["routes"] = operation_routes
		}
	}

	// export array with services and upstreams to the final object
	result["services"] = services
	result["upstreams"] = upstreams

	// we're done!
	return result, nil
}
