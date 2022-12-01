package main

import (
	"fw/convert"
	"log"
)

func main() {
	opts := convert.O2kOptions{
		FilenameIn: "learnservice_oas.yaml",
		ExportYaml: true,
		Tags:       []string{"tag1", "tag2"},
	}

	_, err := convert.ConvertOas3(opts)
	if err != nil {
		log.Fatal(err)
	}
}
