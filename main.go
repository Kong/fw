package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/Kong/fw/convertoas3"
	uuid "github.com/satori/go.uuid"
	"gopkg.in/yaml.v2"
)

const (
	defaultJsonIndent = "  "
)

// mustSerialize will serialize the result as a JSON/YAML. Will panic
// if serializing fails.
func mustSerialize(content map[string]interface{}, asYaml bool) []byte {
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

// mustWriteFile writes the output to a file in JSON/YAML format. Will panic
// if writing fails.
func mustWriteFile(filename string, content []byte) {

	var f *os.File
	var err error

	if filename != "/dev/stdout" {
		// write to file
		f, err = os.Create(filename)
		if err != nil {
			log.Fatalf("failed to create output file '%s'", filename)
		}
		defer f.Close()
	} else {
		// writing to stdout
		f = os.Stdout
	}
	_, err = f.Write(content)
	if err != nil {
		log.Fatalf(fmt.Sprintf("failed to write to output file '%s'; %%w", filename), err)
	}
}

// mustReadFile reads file contents. Will panic if reading fails.
func mustReadFile(filename string) []byte {
	body, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}
	return body
}

func main() {
	// constants for now:
	filenameIn := "/dev/stdin"
	filenameOut := "/dev/stdout"
	asYaml := true
	// tags := []string{"tag1", "tag2"}
	docName := ""
	uuidNamespace := uuid.NamespaceDNS

	// do the work: read/convert/write
	options := convertoas3.O2kOptions{
		// Tags:          &tags,
		DocName:       docName,
		UuidNamespace: uuidNamespace,
	}

	content := mustReadFile(filenameIn)

	result, err := convertoas3.Convert(&content, options)
	if err != nil {
		log.Fatal(err)
	}

	mustWriteFile(filenameOut, mustSerialize(result, asYaml))
}
