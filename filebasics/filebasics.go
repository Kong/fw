package filebasics

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"sigs.k8s.io/yaml"
)

const (
	defaultJSONIndent = "  "
)

// MustReadFile reads file contents. Will panic if reading fails.
// Reads from stdin if filename == "-"
func MustReadFile(filename string) *[]byte {
	if filename == "-" {
		filename = "/dev/stdin" // TODO: this is platform specific!
	}

	body, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}
	return &body
}

// MustWriteFile writes the output to a file. Will panic if writing fails.
// Writes to stdout if filename == "-"
func MustWriteFile(filename string, content *[]byte) {
	var f *os.File
	var err error

	if filename != "-" {
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
	_, err = f.Write(*content)
	if err != nil {
		log.Fatalf(fmt.Sprintf("failed to write to output file '%s'; %%w", filename), err)
	}
}

// MustSerialize will serialize the result as a JSON/YAML. Will panic
// if serializing fails.
func MustSerialize(content map[string]interface{}, asYaml bool) *[]byte {
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
		str, err = json.MarshalIndent(content, "", defaultJSONIndent)
		if err != nil {
			log.Fatal("failed to json-serialize the resulting file; %w", err)
		}
	}

	return &str
}

// MustDeserialize will deserialize data as a JSON or YAML object. Will panic
// if deserializing fails or if it isn't an object. Will never return nil.
func MustDeserialize(data *[]byte) map[string]interface{} {
	var output interface{}

	err1 := json.Unmarshal(*data, &output)
	if err1 != nil {
		err2 := yaml.Unmarshal(*data, &output)
		if err2 != nil {
			log.Fatal("failed deserializing data as JSON (%w) and as YAML (%w)", err1, err2)
		}
	}

	switch output := output.(type) {
	case map[string]interface{}:
		return output
	}

	log.Fatal("Expected the data to be an Object")
	return nil // will never happen, unreachable.
}

// MustWriteSerializedFile will serialize the data and write it to a file. Will
// panic if it fails. Writes to stdout if filename == "-"
func MustWriteSerializedFile(filename string, content map[string]interface{}, asYaml bool) {
	MustWriteFile(filename, MustSerialize(content, asYaml))
}

// MustDeserializeFile will read a JSON or YAML file and return the top-level object. Will
// panic if it fails reading or the content isn't an object. Reads from stdin if filename == "-".
// This will never return nil.
func MustDeserializeFile(filename string) map[string]interface{} {
	return MustDeserialize(MustReadFile(filename))
}
