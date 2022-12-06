package main

import (
	"encoding/json"
	"fmt"
	"fw/convert"
	"log"
	"os"

	uuid "github.com/satori/go.uuid"
	"gopkg.in/yaml.v2"
)

const (
	defaultJsonIndent = "  "
)

// serialize will serialize the result as a JSON/YAML. Will panic
// if serializing fails.
func serialize(content map[string]interface{}, asYaml bool) []byte {
	var (
		str []byte
		err error
	)

	if asYaml {
		str, err = yaml.Marshal(content)
		if err != nil {
			log.Fatal("failed to yaml-serialize the resulting file; %w", err)
		}
	} else {
		str, err = json.MarshalIndent(content, "", defaultJsonIndent)
		if err != nil {
			log.Fatal("failed to json-serialize the resulting file; %w", err)
		}
	}

	return str
}

// writeFile writes the output to a file in JSON/YAML format. Will panic
// if writing fails.
func writeFile(filename string, content []byte) {

	// write to file
	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("failed to create output file '%s'", filename)
	}
	defer f.Close()
	_, err = f.Write(content)
	if err != nil {
		log.Fatalf(fmt.Sprintf("failed to write to output file '%s'; %%w", filename), err)
	}
}

// readFile reads file contents. Will panic if reading fails.
func readFile(filename string) []byte {
	body, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}
	return body
}

func main() {
	// constants for now:
	filenameIn := "learnservice_oas.yaml"
	filenameOut := "/dev/stdout"
	asYaml := true
	tags := []string{"tag1", "tag2"}
	docName := ""
	uuidNamespace := uuid.NamespaceDNS

	// do the work: read/convert/write
	options := convert.O2kOptions{
		Tags:          tags,
		DocName:       docName,
		UuidNamespace: uuidNamespace,
	}

	content := readFile(filenameIn)

	result, err := convert.ConvertOas3(&content, options)
	if err != nil {
		log.Fatal(err)
	}

	writeFile(filenameOut, serialize(result, asYaml))
}
