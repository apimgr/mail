//go:build tools

// Command validate checks that the schema-first SDL matches its Go resolvers.
package main

import (
	"fmt"
	"os"

	mailgraphql "github.com/apimgr/mail/src/graphql"
)

func main() {
	if _, err := mailgraphql.NewSchema(nil, nil); err != nil {
		fmt.Fprintf(os.Stderr, "GraphQL schema validation failed: %v\n", err)
		os.Exit(1)
	}
}
