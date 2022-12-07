package convert

import (
	"encoding/json"
	"fmt"
	"fw/kong"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	uuid "github.com/satori/go.uuid"
)

const (
	formatVersionKey   = "_format_version"
	formatVersionValue = "3.0"
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

func isJsonObject(json []byte) bool {
	return true // TODO: implement
}

func getXKongObjectDefaults(props openapi3.ExtensionProps, name string) (string, error) {
	if props.Extensions != nil && props.Extensions[name] != nil {
		jsonblob, _ := json.Marshal(props.Extensions[name])
		if !isJsonObject(jsonblob) {
			return "", fmt.Errorf("expected '%s' to be a JSON object", name)
		}
		return string(jsonblob), nil
	}
	return "", nil
}

// getServiceDefaults returns a JSON string containing the defaults
func getServiceDefaults(props openapi3.ExtensionProps) (string, error) {
	return getXKongObjectDefaults(props, "x-kong-service-defaults")
}

// getUpstreamDefaults returns a JSON string containing the defaults
func getUpstreamDefaults(props openapi3.ExtensionProps) (string, error) {
	return getXKongObjectDefaults(props, "x-kong-upstream-defaults")
}

// getRouteDefaults returns a JSON string containing the defaults
func getRouteDefaults(props openapi3.ExtensionProps) (string, error) {
	return getXKongObjectDefaults(props, "x-kong-route-defaults")
}

// ConvertOas3 converts an OpenAPI spec to a Kong declarative file.
func ConvertOas3(content *[]byte, opts O2kOptions) (map[string]interface{}, error) {
	opts.setDefaults()

	// set up output document
	result := make(map[string]interface{})
	result[formatVersionKey] = formatVersionValue
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
		docRouteDefaults    string                 // JSON string representation of route-defaults on document level

		pathServers          *openapi3.Servers      // servers block on current path level
		pathServiceDefaults  string                 // JSON string representation of service-defaults on path level
		pathService          map[string]interface{} // service entity in use on path level
		pathUpstreamDefaults string                 // JSON string representation of upstream-defaults on path level
		pathUpstream         map[string]interface{} // upstream entity in use on path level
		pathRouteDefaults    string                 // JSON string representation of route-defaults on path level

		operationServers          *openapi3.Servers      // servers block on current operation level
		operationServiceDefaults  string                 // JSON string representation of service-defaults on operation level
		operationService          map[string]interface{} // service entity in use on operation level
		operationUpstreamDefaults string                 // JSON string representation of upstream-defaults on operation level
		operationUpstream         map[string]interface{} // upstream entity in use on operation level
		operationRouteDefaults    string                 // JSON string representation of route-defaults on operation level
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
			// TODO: the access below is unsafe if intermediate levels do not exist and might cause a panic (check other instances as well!)
			err = json.Unmarshal(doc.ExtensionProps.Extensions["x-kong-name"].(json.RawMessage), &opts.DocName)
			if err != nil {
				return nil, fmt.Errorf("expected 'x-kong-name' to be a string; %w", err)
			}
		} else {
			opts.DocName = doc.Info.Title
		}
	}

	// for defaults we keep strings, so deserializing them provides a copy right away
	if docServiceDefaults, err = getServiceDefaults(doc.ExtensionProps); err != nil {
		return nil, err
	}
	if docUpstreamDefaults, err = getUpstreamDefaults(doc.ExtensionProps); err != nil {
		return nil, err
	}
	if docRouteDefaults, err = getRouteDefaults(doc.ExtensionProps); err != nil {
		return nil, err
	}

	// create the top-level docService and (optional) docUpstream
	docService, docUpstream, err = kong.CreateKongService(opts.DocName, docServers, docServiceDefaults, docUpstreamDefaults, opts.Tags, opts.UuidNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create service/upstream from document root: %w", err)
	}
	services = append(services, docService)
	if docUpstream != nil {
		upstreams = append(upstreams, docUpstream)
	}

	for path, pathitem := range doc.Paths {

		// Set up the defaults on the Path level
		newService := false
		if pathServiceDefaults, err = getServiceDefaults(pathitem.ExtensionProps); err != nil {
			return nil, err
		}
		if pathServiceDefaults == "" {
			pathServiceDefaults = docServiceDefaults
		} else {
			newService = true
		}

		if pathUpstreamDefaults, err = getUpstreamDefaults(pathitem.ExtensionProps); err != nil {
			return nil, err
		}
		if pathUpstreamDefaults == "" {
			pathUpstreamDefaults = docUpstreamDefaults
		} else {
			newService = true
		}

		if pathRouteDefaults, err = getRouteDefaults(pathitem.ExtensionProps); err != nil {
			return nil, err
		}
		if pathRouteDefaults == "" {
			pathRouteDefaults = docRouteDefaults
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
			if operationServiceDefaults, err = getServiceDefaults(operation.ExtensionProps); err != nil {
				return nil, err
			}
			if operationServiceDefaults == "" {
				operationServiceDefaults = pathServiceDefaults
			} else {
				newService = true
			}

			if operationUpstreamDefaults, err = getUpstreamDefaults(operation.ExtensionProps); err != nil {
				return nil, err
			}
			if operationUpstreamDefaults == "" {
				operationUpstreamDefaults = pathUpstreamDefaults
			} else {
				newService = true
			}

			if operationRouteDefaults, err = getRouteDefaults(operation.ExtensionProps); err != nil {
				return nil, err
			}
			if operationRouteDefaults == "" {
				operationRouteDefaults = pathRouteDefaults
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

			// construct the route
			var route map[string]interface{}
			if operationRouteDefaults != "" {
				json.Unmarshal([]byte(operationRouteDefaults), &route)
			}

			// TODO: create and add an ID
			route["id"] = "v5 uuid"

			// TODO: create and add a route-name, using operation id
			route["name"] = "route-name-tbd"

			// convert path parameters to regex captures
			re, _ := regexp.Compile("{([^}]+)}")
			if matches := re.FindAllString(path, -1); matches != nil {
				for _, varName := range matches {
					placeHolder := "{" + varName + "}"
					regexMatch := "(?<" + varName + ">[^/]+)"
					path = strings.Replace(path, placeHolder, regexMatch, 1)
				}
			}
			route["paths"] = []string{"~" + path + "$"}
			route["methods"] = []string{method}
			route["tags"] = opts.Tags
			route["strip_path"] = false // TODO: there should be some logic around defaults etc iirc

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
