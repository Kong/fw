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
	var emptyUuid uuid.UUID
	if uuid.Equal(emptyUuid, opts.UuidNamespace) {
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

	var (
		err error
		doc *openapi3.T // The OAS3 document we're operating on

		docServers          *openapi3.Servers      // servers block on document level
		docServiceDefaults  string                 // JSON string representation of service-defaults on document level
		docService          map[string]interface{} // service entity in use on document level
		docUpstreamDefaults string                 // JSON string representation of upstream-defaults on document level
		docUpstream         map[string]interface{} // upstream entity in use on document level

		pathServers          *openapi3.Servers      // servers block on current path level
		pathServiceDefaults  string                 // JSON string representation of service-defaults on path level
		pathService          map[string]interface{} // service entity in use on path level
		pathUpstreamDefaults string                 // JSON string representation of upstream-defaults on path level
		pathUpstream         map[string]interface{} // upstream entity in use on path level

		operationServers          *openapi3.Servers      // servers block on current operation level
		operationServiceDefaults  string                 // JSON string representation of service-defaults on operation level
		operationService          map[string]interface{} // service entity in use on operation level
		operationUpstreamDefaults string                 // JSON string representation of upstream-defaults on operation level
		operationUpstream         map[string]interface{} // upstream entity in use on operation level
	)

	// Load and parse the OAS file
	loader := openapi3.NewLoader()
	doc, err = loader.LoadFromData(*content)
	if err != nil {
		return nil, fmt.Errorf("error parsing OAS3 file: [%w]", err)
	}

	// set document level elements
	docServers = &doc.Servers // this one is always set, but can be empty

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
	if doc.ExtensionProps.Extensions["x-kong-service-defaults"] != nil {
		jsonblob, _ := json.Marshal(doc.ExtensionProps.Extensions["x-kong-service-defaults"])
		docServiceDefaults = string(jsonblob)
	} else {
		docServiceDefaults = "{}" // just empty JSON object
	}

	if doc.ExtensionProps.Extensions["x-kong-upstream-defaults"] != nil {
		jsonblob, _ := json.Marshal(doc.ExtensionProps.Extensions["x-kong-upstream-defaults"])
		docUpstreamDefaults = string(jsonblob)
	} else {
		docUpstreamDefaults = ""
	}

	// create the top-level docService and (optional) docUpstream
	docService, docUpstream, err = kong.CreateKongService(opts.DocName, docServers, docServiceDefaults, docUpstreamDefaults, opts.Tags, opts.UuidNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create service/updstream from document root: %w", err)
	}
	services = append(services, docService)
	if docUpstream != nil {
		upstreams = append(upstreams, docUpstream)
	}

	for path, pathitem := range doc.Paths {

		// Set up the defaults on the Path level
		newService := false
		if pathitem.ExtensionProps.Extensions["x-kong-service-defaults"] != nil {
			jsonblob, _ := json.Marshal(pathitem.ExtensionProps.Extensions["x-kong-service-defaults"])
			pathServiceDefaults = string(jsonblob)
			newService = true
		} else {
			pathServiceDefaults = docServiceDefaults
		}

		if pathitem.ExtensionProps.Extensions["x-kong-upstream-defaults"] != nil {
			jsonblob, _ := json.Marshal(pathitem.ExtensionProps.Extensions["x-kong-upstream-defaults"])
			pathUpstreamDefaults = string(jsonblob)
			newService = true
		} else {
			pathUpstreamDefaults = docUpstreamDefaults
		}

		// if there is no path level servers block, use the document one
		pathServers = &pathitem.Servers
		if len(*pathServers) == 0 { // it's always set, so we ignore it if empty
			pathServers = docServers
		} else {
			newService = true
		}

		// create a new service if we need to do so
		if newService {
			// create the path-level service and (optional) upstream
			// TODO: the path ends up with / in the hostname of the service
			pathService, pathUpstream, err = kong.CreateKongService(
				opts.DocName+"_"+path,
				pathServers,
				pathServiceDefaults,
				pathUpstreamDefaults,
				opts.Tags,
				opts.UuidNamespace)
			if err != nil {
				return nil, fmt.Errorf("failed to create service/updstream from path '%s': %w", path, err)
			}
			services = append(services, pathService)
			if pathUpstream != nil {
				upstreams = append(upstreams, pathUpstream)
			}
		} else {
			pathService = docService
		}

		// traverse all operations

		for method, operation := range pathitem.Operations() {

			var operationRoutes []interface{} // the routes array we need to add to

			// Set up the defaults on the Operation level
			newService := false
			if operation.ExtensionProps.Extensions["x-kong-service-defaults"] != nil {
				jsonblob, _ := json.Marshal(operation.ExtensionProps.Extensions["x-kong-service-defaults"])
				operationServiceDefaults = string(jsonblob)
				newService = true
			} else {
				operationServiceDefaults = pathServiceDefaults
			}

			if operation.ExtensionProps.Extensions["x-kong-upstream-defaults"] != nil {
				jsonblob, _ := json.Marshal(operation.ExtensionProps.Extensions["x-kong-upstream-defaults"])
				operationUpstreamDefaults = string(jsonblob)
				newService = true
			} else {
				operationUpstreamDefaults = pathUpstreamDefaults
			}

			// if there is no operation level servers block, use the path one
			operationServers = operation.Servers
			if operationServers == nil || len(*operationServers) == 0 {
				operationServers = pathServers
			} else {
				newService = true
			}

			// create a new service if we need to do so
			if newService {
				// create the operation-level service and (optional) upstream
				// TODO: the path ends up with / in the hostname of the service
				operationService, operationUpstream, err = kong.CreateKongService(
					opts.DocName+"_"+path+"_"+method, //TODO: use operation ID if available
					operationServers,
					operationServiceDefaults,
					operationUpstreamDefaults,
					opts.Tags,
					opts.UuidNamespace)
				if err != nil {
					return nil, fmt.Errorf("failed to create service/updstream from operation '%s %s': %w", path, method, err)
				}
				services = append(services, operationService)
				if operationUpstream != nil {
					upstreams = append(upstreams, operationUpstream)
				}
				operationRoutes = operationService["routes"].([]interface{})
			} else {
				operationService = pathService
				operationRoutes = operationService["routes"].([]interface{})
			}

			// TODO: add route-defaults on all levels

			// prefix, _ := operationServers.BasePath()
			// println(method, prefix, path)

			// construct the route
			route := make(map[string]interface{}) // the newly generated Route // TODO: create it from route-defaults
			// TODO: create and add a route-name, using operation id

			// TODO: create and add an ID
			route["paths"] = []string{path} // TODO: convert path to regex before use, or to new router DSL
			route["methods"] = []string{method}
			route["tags"] = opts.Tags

			operationRoutes = append(operationRoutes, route)
			operationService["routes"] = operationRoutes
		}
	}

	// export array with services and upstreams to the final object
	result["services"] = services
	result["upstreams"] = upstreams

	// we're done!
	return result, nil
}
