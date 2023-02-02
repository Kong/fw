package main

import (
	"github.com/Kong/fw/convertoas3"
	"github.com/Kong/fw/filebasics"
	uuid "github.com/satori/go.uuid"
)

func main() {
	// constants for now:
	filenameIn := "-"
	filenameOut := "-"
	asYaml := true
	// tags := []string{"tag1", "tag2"}
	docName := ""
	uuidNamespace := uuid.NamespaceDNS

	// do the work: read/convert/write
	options := convertoas3.O2kOptions{
		// Tags:          &tags,
		DocName:       docName,
		UUIDNamespace: uuidNamespace,
	}

	deckData := convertoas3.MustConvert(filebasics.MustReadFile(filenameIn), options)
	filebasics.MustWriteSerializedFile(filenameOut, deckData, asYaml)
}
