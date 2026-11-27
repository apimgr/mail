// Package graphql defines the schema-first, query-only mail API schema.
package graphql

//go:generate go run -tags tools ./cmd/validate

import (
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/mailservice"
	graphqlgo "github.com/graph-gophers/graphql-go"
)

// Schema is the parsed GraphQL schema served by the API.
type Schema = graphqlgo.Schema

// NewSchema builds the code-backed GraphQL schema for the current mail API.
func NewSchema(db *database.DB, queue mailservice.QueueManager) (*graphqlgo.Schema, error) {
	return graphqlgo.ParseSchema(schemaSDL, &queryResolver{db: db, queue: queue},
		graphqlgo.UseFieldResolvers(), graphqlgo.MaxDepth(8), graphqlgo.MaxQueryLength(32<<10), graphqlgo.MaxParallelism(8),
	)
}

// SchemaSDL exposes the source schema for drift tests and documentation tooling.
func SchemaSDL() string { return schemaSDL }
