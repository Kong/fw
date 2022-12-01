package kong

import (
	"strings"

	"github.com/mozillazg/go-slugify"
)

// Converts a name to a valid Kong name by removing and replacing unallowed characters
// and sanitizing non-latin characters
func Slugify(name ...string) string {

	for i, elem := range name {
		name[i] = slugify.Slugify(elem)
	}

	return strings.Join(name, "_")
}
