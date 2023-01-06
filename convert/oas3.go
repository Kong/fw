package convert

import (
	"encoding/json"
	"fmt"
	"fw/kong"
	"regexp"
	"sort"
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
	Tags          *[]string // Array of tags to mark all generated entities with
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

// getKongName returns the provided tags or if nil, then the `x-kong-tags` property,
// validated to be a string array. If there is no error, then there will always be
// an array returned for safe access later in the process.
func getKongTags(doc *openapi3.T, tagsProvided *[]string) ([]string, error) {
	if tagsProvided != nil {
		// the provided tags take precedence, return them
		return *tagsProvided, nil
	}

	if doc.ExtensionProps.Extensions == nil || doc.ExtensionProps.Extensions["x-kong-tags"] == nil {
		// there is no extension, so return an empty array
		return make([]string, 0), nil
	}

	var tagsValue interface{}
	err := json.Unmarshal(doc.ExtensionProps.Extensions["x-kong-tags"].(json.RawMessage), &tagsValue)
	if err != nil {
		return nil, fmt.Errorf("expected 'x-kong-tags' to be an array of strings: %w", err)
	}
	var tagsArray []interface{}
	switch tags := tagsValue.(type) {
	case []interface{}:
		// got a proper array
		tagsArray = tags
	default:
		return nil, fmt.Errorf("expected 'x-kong-tags' to be an array of strings")
	}

	resultArray := make([]string, len(tagsArray))
	for i := 0; i < len(tagsArray); i++ {
		switch tag := tagsArray[i].(type) {
		case string:
			resultArray[i] = tag
		default:
			return nil, fmt.Errorf("expected 'x-kong-tags' to be an array of strings")
		}
	}
	return resultArray, nil
}

// getKongName returns the `x-kong-name` property, validated to be a string
func getKongName(props openapi3.ExtensionProps) (string, error) {
	if props.Extensions != nil && props.Extensions["x-kong-name"] != nil {
		var name string
		err := json.Unmarshal(props.Extensions["x-kong-name"].(json.RawMessage), &name)
		if err != nil {
			return "", fmt.Errorf("expected 'x-kong-name' to be a string: %w", err)
		}
		return name, nil
	}
	return "", nil
}

func dereferenceJsonObject(value map[string]interface{}, components *map[string]interface{}) (map[string]interface{}, error) {
	var pointer string

	switch value["$ref"].(type) {
	case nil: // it is not a reference, so return the object
		return value, nil

	case string: // it is a json pointer
		pointer = value["$ref"].(string)
		if !strings.HasPrefix(pointer, "#/components/x-kong/") {
			return nil, fmt.Errorf("all 'x-kong-...' references must be at '#/components/x-kong/...'")
		}

	default: // bad pointer
		return nil, fmt.Errorf("expected '$ref' pointer to be a string")
	}

	// walk the tree to find the reference
	segments := strings.Split(pointer, "/")
	path := "#/components/x-kong"
	result := components

	for i := 3; i < len(segments); i++ {
		segment := segments[i]
		path = path + "/" + segment

		switch (*result)[segment].(type) {
		case nil:
			return nil, fmt.Errorf("reference '%s' not found", pointer)
		case map[string]interface{}:
			target := (*result)[segment].(map[string]interface{})
			result = &target
		default:
			return nil, fmt.Errorf("expected '%s' to be a JSON object", path)
		}
	}

	return *result, nil
}

func toJsonObject(object interface{}) (map[string]interface{}, error) {
	switch result := object.(type) {
	case map[string]interface{}:
		return result, nil
	default:
		return nil, fmt.Errorf("not a Json object")
	}
}

// getXKongObject returns specified 'key' from the extension properties if available.
// returns nil if it wasn't found, an error if it wasn't an object or couldn't be
// dereferenced. The returned object will be json encoded again.
func getXKongObject(props openapi3.ExtensionProps, key string, components *map[string]interface{}) ([]byte, error) {
	if props.Extensions != nil && props.Extensions[key] != nil {
		var jsonBlob interface{}
		json.Unmarshal(props.Extensions[key].(json.RawMessage), &jsonBlob)
		jsonObject, err := toJsonObject(jsonBlob)
		if err != nil {
			return nil, fmt.Errorf("expected '%s' to be a JSON object", key)
		}

		object, err := dereferenceJsonObject(jsonObject, components)
		if err != nil {
			return nil, err
		}
		return json.Marshal(object)
	}
	return nil, nil
}

// getXKongComponents will return a map of the '/components/x-kong/' object. If
// the extension is not there it will return an empty map. If the entry is not a
// Json object, it will return an error.
func getXKongComponents(doc *openapi3.T) (*map[string]interface{}, error) {
	var components map[string]interface{}
	switch prop := doc.Components.ExtensionProps.Extensions["x-kong"].(type) {
	case nil:
		// not available, create empty map to do safe lookups down the line
		components = make(map[string]interface{})

	default:
		// we got some json blob
		var xKong interface{}
		json.Unmarshal(prop.(json.RawMessage), &xKong)

		switch val := xKong.(type) {
		case map[string]interface{}:
			components = val

		default:
			return nil, fmt.Errorf("expected '/components/x-kong' to be a JSON object")
		}
	}

	return &components, nil
}

// getServiceDefaults returns a JSON string containing the defaults
func getServiceDefaults(props openapi3.ExtensionProps, components *map[string]interface{}) ([]byte, error) {
	return getXKongObject(props, "x-kong-service-defaults", components)
}

// getUpstreamDefaults returns a JSON string containing the defaults
func getUpstreamDefaults(props openapi3.ExtensionProps, components *map[string]interface{}) ([]byte, error) {
	return getXKongObject(props, "x-kong-upstream-defaults", components)
}

// getRouteDefaults returns a JSON string containing the defaults
func getRouteDefaults(props openapi3.ExtensionProps, components *map[string]interface{}) ([]byte, error) {
	return getXKongObject(props, "x-kong-route-defaults", components)
}

// create plugin id
func createPluginId(uuidNamespace uuid.UUID, baseName string, config map[string]interface{}) string {
	pluginName := config["name"].(string) // safe because it was previously parsed

	return uuid.NewV5(uuidNamespace, baseName+".plugin."+pluginName).String()
}

// getPluginsList returns a list of plugins retrieved from the extension properties
// (the 'x-kong-plugin<pluginname>' extensions). Applied on top of the optional
// pluginsToInclude list. The result will be sorted by plugin name.
func getPluginsList(
	props openapi3.ExtensionProps,
	pluginsToInclude *[]*map[string]interface{},
	uuidNamespace uuid.UUID,
	baseName string,
	components *map[string]interface{}) (*[]*map[string]interface{}, error) {

	plugins := make(map[string]*map[string]interface{})

	// copy inherited list of plugins
	if pluginsToInclude != nil {
		for _, config := range *pluginsToInclude {
			pluginName := (*config)["name"].(string) // safe because it was previously parsed

			// serialize/deserialize to create a deep-copy
			var configCopy map[string]interface{}
			jConf, _ := json.Marshal(config)
			json.Unmarshal(jConf, &configCopy)

			// generate a new ID, for a new plugin, based on new basename
			configCopy["id"] = createPluginId(uuidNamespace, baseName, configCopy)

			plugins[pluginName] = &configCopy
		}
	}

	if props.Extensions != nil {
		// there are extensions, go check if there are plugins
		for extensionName := range props.Extensions {

			if strings.HasPrefix(extensionName, "x-kong-plugin-") {
				pluginName := strings.TrimPrefix(extensionName, "x-kong-plugin-")

				jsonstr, err := getXKongObject(props, extensionName, components)
				if err != nil {
					return nil, err
				}

				var pluginConfig map[string]interface{}
				err = json.Unmarshal([]byte(jsonstr), &pluginConfig)
				// err := json.Unmarshal(jsonBytes.(json.RawMessage), &pluginConfig)
				if err != nil {
					return nil, fmt.Errorf(fmt.Sprintf("failed to parse JSON object for '%s': %%w", extensionName), err)
				}

				if pluginConfig["name"] == nil {
					pluginConfig["name"] = pluginName
				} else {
					if pluginConfig["name"] != pluginName {
						return nil, fmt.Errorf("extension '%s' specifies a different name than the config; '%s'", extensionName, pluginName)
					}
				}
				pluginConfig["id"] = createPluginId(uuidNamespace, baseName, pluginConfig)

				plugins[pluginName] = &pluginConfig
			}
		}
	}

	// the list is complete, sort to be deterministic in the output
	sortedNames := make([]string, len(plugins))
	i := 0
	for pluginName := range plugins {
		sortedNames[i] = pluginName
		i++
	}
	sort.Strings(sortedNames)

	sorted := make([]*map[string]interface{}, len(plugins))
	for i, pluginName := range sortedNames {
		sorted[i] = plugins[pluginName]
	}
	return &sorted, nil
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
		err            error
		doc            *openapi3.T             // the OAS3 document we're operating on
		kongComponents *map[string]interface{} // contents of OAS key `/components/x-kong/`
		kongTags       []string                // tags to attach to Kong entities

		docBaseName         string                     // the slugified basename for the document
		docServers          *openapi3.Servers          // servers block on document level
		docServiceDefaults  []byte                     // JSON string representation of service-defaults on document level
		docService          map[string]interface{}     // service entity in use on document level
		docUpstreamDefaults []byte                     // JSON string representation of upstream-defaults on document level
		docUpstream         map[string]interface{}     // upstream entity in use on document level
		docRouteDefaults    []byte                     // JSON string representation of route-defaults on document level
		docPluginList       *[]*map[string]interface{} // array of plugin configs, sorted by plugin name

		pathBaseName         string                     // the slugified basename for the path
		pathServers          *openapi3.Servers          // servers block on current path level
		pathServiceDefaults  []byte                     // JSON string representation of service-defaults on path level
		pathService          map[string]interface{}     // service entity in use on path level
		pathUpstreamDefaults []byte                     // JSON string representation of upstream-defaults on path level
		pathUpstream         map[string]interface{}     // upstream entity in use on path level
		pathRouteDefaults    []byte                     // JSON string representation of route-defaults on path level
		pathPluginList       *[]*map[string]interface{} // array of plugin configs, sorted by plugin name

		operationBaseName         string                     // the slugified basename for the operation
		operationServers          *openapi3.Servers          // servers block on current operation level
		operationServiceDefaults  []byte                     // JSON string representation of service-defaults on operation level
		operationService          map[string]interface{}     // service entity in use on operation level
		operationUpstreamDefaults []byte                     // JSON string representation of upstream-defaults on operation level
		operationUpstream         map[string]interface{}     // upstream entity in use on operation level
		operationRouteDefaults    []byte                     // JSON string representation of route-defaults on operation level
		operationPluginList       *[]*map[string]interface{} // array of plugin configs, sorted by plugin name
	)

	// Load and parse the OAS file
	loader := openapi3.NewLoader()
	doc, err = loader.LoadFromData(*content)
	if err != nil {
		return nil, fmt.Errorf("error parsing OAS3 file: [%w]", err)
	}

	//
	//
	//  Handle OAS Document level
	//
	//

	// collect tags to use
	if kongTags, err = getKongTags(doc, opts.Tags); err != nil {
		return nil, err
	}

	// set document level elements
	docServers = &doc.Servers // this one is always set, but can be empty

	// determine document name, precedence: specified -> x-kong-name -> Info.Title
	docBaseName = opts.DocName
	if docBaseName == "" {
		if docBaseName, err = getKongName(doc.ExtensionProps); err != nil {
			return nil, err
		}
		if docBaseName == "" {
			docBaseName = doc.Info.Title
		}
	}
	docBaseName = kong.Slugify(docBaseName)

	if kongComponents, err = getXKongComponents(doc); err != nil {
		return nil, err
	}

	// for defaults we keep strings, so deserializing them provides a copy right away
	if docServiceDefaults, err = getServiceDefaults(doc.ExtensionProps, kongComponents); err != nil {
		return nil, err
	}
	if docUpstreamDefaults, err = getUpstreamDefaults(doc.ExtensionProps, kongComponents); err != nil {
		return nil, err
	}
	if docRouteDefaults, err = getRouteDefaults(doc.ExtensionProps, kongComponents); err != nil {
		return nil, err
	}

	// create the top-level docService and (optional) docUpstream
	docService, docUpstream, err = kong.CreateKongService(docBaseName, docServers, docServiceDefaults, docUpstreamDefaults, kongTags, opts.UuidNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create service/upstream from document root: %w", err)
	}
	services = append(services, docService)
	if docUpstream != nil {
		upstreams = append(upstreams, docUpstream)
	}

	// attach plugins
	docPluginList, err = getPluginsList(doc.ExtensionProps, nil, opts.UuidNamespace, docBaseName, kongComponents)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugins list from document root: %w", err)
	}
	docService["plugins"] = docPluginList

	//
	//
	//  Handle OAS Path level
	//
	//

	// create a sorted array of paths, to be deterministic in our output order
	sortedPaths := make([]string, len(doc.Paths))
	i := 0
	for path := range doc.Paths {
		sortedPaths[i] = path
		i++
	}
	sort.Strings(sortedPaths)

	for _, path := range sortedPaths {
		pathitem := doc.Paths[path]

		// determine path name, precedence: specified -> x-kong-name -> actual-path
		if pathBaseName, err = getKongName(pathitem.ExtensionProps); err != nil {
			return nil, err
		}
		if pathBaseName == "" {
			pathBaseName = path
		}
		pathBaseName = docBaseName + "_" + kong.Slugify(pathBaseName)

		// Set up the defaults on the Path level
		newPathService := false
		if pathServiceDefaults, err = getServiceDefaults(pathitem.ExtensionProps, kongComponents); err != nil {
			return nil, err
		}
		if pathServiceDefaults == nil {
			pathServiceDefaults = docServiceDefaults
		} else {
			newPathService = true
		}

		newUpstream := false
		if pathUpstreamDefaults, err = getUpstreamDefaults(pathitem.ExtensionProps, kongComponents); err != nil {
			return nil, err
		}
		if pathUpstreamDefaults == nil {
			pathUpstreamDefaults = docUpstreamDefaults
		} else {
			newUpstream = true
			newPathService = true
		}

		if pathRouteDefaults, err = getRouteDefaults(pathitem.ExtensionProps, kongComponents); err != nil {
			return nil, err
		}
		if pathRouteDefaults == nil {
			pathRouteDefaults = docRouteDefaults
		}

		// if there is no path level servers block, use the document one
		pathServers = &pathitem.Servers
		if len(*pathServers) == 0 { // it's always set, so we ignore it if empty
			pathServers = docServers
		} else {
			newUpstream = true
			newPathService = true
		}

		// create a new service if we need to do so
		if newPathService {
			// create the path-level service and (optional) upstream
			pathService, pathUpstream, err = kong.CreateKongService(
				pathBaseName,
				pathServers,
				pathServiceDefaults,
				pathUpstreamDefaults,
				kongTags,
				opts.UuidNamespace)
			if err != nil {
				return nil, fmt.Errorf("failed to create service/updstream from path '%s': %w", path, err)
			}

			// collect path plugins, including the doc-level plugins since we have a new service entity
			pathPluginList, err = getPluginsList(pathitem.ExtensionProps, docPluginList, opts.UuidNamespace, pathBaseName, kongComponents)
			if err != nil {
				return nil, fmt.Errorf("failed to create plugins list from path item: %w", err)
			}
			pathService["plugins"] = pathPluginList

			services = append(services, pathService)
			if pathUpstream != nil {
				// we have a new upstream, but do we need it?
				if newUpstream {
					// we need it, so store and use it
					upstreams = append(upstreams, pathUpstream)
				} else {
					// we don't need it, so update service to point to 'upper' upstream
					pathService["host"] = docService["host"]
				}
			}
		} else {
			// no new path-level service entity required, so stick to the doc-level one
			pathService = docService

			// collect path plugins, only the path level, since we're on the doc-level service-entity
			pathPluginList, err = getPluginsList(pathitem.ExtensionProps, nil, opts.UuidNamespace, pathBaseName, kongComponents)
			if err != nil {
				return nil, fmt.Errorf("failed to create plugins list from path item: %w", err)
			}
		}

		//
		//
		//  Handle OAS Operation level
		//
		//

		// create a sorted array of operations, to be deterministic in our output order
		operations := pathitem.Operations()
		sortedMethods := make([]string, len(operations))
		i := 0
		for method := range operations {
			sortedMethods[i] = method
			i++
		}
		sort.Strings(sortedMethods)

		// traverse all operations
		for _, method := range sortedMethods {
			operation := operations[method]

			var operationRoutes []interface{} // the routes array we need to add to

			// determine operation name, precedence: specified -> operation-ID -> method-name
			if operationBaseName, err = getKongName(operation.ExtensionProps); err != nil {
				return nil, err
			}
			if operationBaseName != "" {
				// an x-kong-name was provided, so build as "doc-path-name"
				operationBaseName = pathBaseName + "_" + kong.Slugify(operationBaseName)
			} else {
				operationBaseName = operation.OperationID
				if operationBaseName == "" {
					// no operation ID provided, so build as "doc-path-method"
					operationBaseName = pathBaseName + "_" + kong.Slugify(method)
				} else {
					// operation ID is provided, so build as "doc-operationid"
					operationBaseName = docBaseName + "_" + kong.Slugify(operationBaseName)
				}
			}

			// Set up the defaults on the Operation level
			newOperationService := false
			if operationServiceDefaults, err = getServiceDefaults(operation.ExtensionProps, kongComponents); err != nil {
				return nil, err
			}
			if operationServiceDefaults == nil {
				operationServiceDefaults = pathServiceDefaults
			} else {
				newOperationService = true
			}

			newUpstream := false
			if operationUpstreamDefaults, err = getUpstreamDefaults(operation.ExtensionProps, kongComponents); err != nil {
				return nil, err
			}
			if operationUpstreamDefaults == nil {
				operationUpstreamDefaults = pathUpstreamDefaults
			} else {
				newUpstream = true
				newOperationService = true
			}

			if operationRouteDefaults, err = getRouteDefaults(operation.ExtensionProps, kongComponents); err != nil {
				return nil, err
			}
			if operationRouteDefaults == nil {
				operationRouteDefaults = pathRouteDefaults
			}

			// if there is no operation level servers block, use the path one
			operationServers = operation.Servers
			if operationServers == nil || len(*operationServers) == 0 {
				operationServers = pathServers
			} else {
				newUpstream = true
				newOperationService = true
			}

			// create a new service if we need to do so
			if newOperationService {
				// create the operation-level service and (optional) upstream
				operationService, operationUpstream, err = kong.CreateKongService(
					operationBaseName,
					operationServers,
					operationServiceDefaults,
					operationUpstreamDefaults,
					kongTags,
					opts.UuidNamespace)
				if err != nil {
					return nil, fmt.Errorf("failed to create service/updstream from operation '%s %s': %w", path, method, err)
				}
				services = append(services, operationService)
				if operationUpstream != nil {
					// we have a new upstream, but do we need it?
					if newUpstream {
						// we need it, so store and use it
						upstreams = append(upstreams, operationUpstream)
					} else {
						// we don't need it, so update service to point to 'upper' upstream
						operationService["host"] = pathService["host"]
					}
				}
				operationRoutes = operationService["routes"].([]interface{})
			} else {
				operationService = pathService
				operationRoutes = operationService["routes"].([]interface{})
			}

			// collect operation plugins
			if !newOperationService && !newPathService {
				// we're operating on the doc-level service entity, so we need the plugins
				// from the path and operation
				operationPluginList, err = getPluginsList(operation.ExtensionProps, pathPluginList, opts.UuidNamespace, operationBaseName, kongComponents)
			} else if newOperationService {
				// we're operating on an operation-level service entity, so we need the plugins
				// from the document, path, and operation.
				operationPluginList, _ = getPluginsList(doc.ExtensionProps, nil, opts.UuidNamespace, operationBaseName, kongComponents)
				operationPluginList, _ = getPluginsList(pathitem.ExtensionProps, operationPluginList, opts.UuidNamespace, operationBaseName, kongComponents)
				operationPluginList, err = getPluginsList(operation.ExtensionProps, operationPluginList, opts.UuidNamespace, operationBaseName, kongComponents)
			} else if newPathService {
				// we're operating on a path-level service entity, so we only need the plugins
				// from the operation.
				operationPluginList, err = getPluginsList(operation.ExtensionProps, nil, opts.UuidNamespace, operationBaseName, kongComponents)
			}
			if err != nil {
				return nil, fmt.Errorf("failed to create plugins list from operation item: %w", err)
			}

			// construct the route
			var route map[string]interface{}
			if operationRouteDefaults != nil {
				json.Unmarshal(operationRouteDefaults, &route)
			} else {
				route = make(map[string]interface{})
			}

			// attach the collected plugins configs to the route
			route["plugins"] = operationPluginList

			// convert path parameters to regex captures
			re, _ := regexp.Compile("{([^}]+)}")
			if matches := re.FindAllStringSubmatch(path, -1); matches != nil {
				for _, match := range matches {
					varName := match[1]
					// match single segment; '/', '?', and '#' can mark the end of a segment
					// see https://github.com/OAI/OpenAPI-Specification/issues/291#issuecomment-316593913
					regexMatch := "(?<" + varName + ">[^#?/]+)"
					placeHolder := "{" + varName + "}"
					path = strings.Replace(path, placeHolder, regexMatch, 1)
				}
			}
			route["paths"] = []string{"~" + path + "$"}
			route["id"] = uuid.NewV5(opts.UuidNamespace, operationBaseName+".route").String()
			route["name"] = operationBaseName
			route["methods"] = []string{method}
			route["tags"] = kongTags
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
