// Package generated contains GraphQL generated code
package generated

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/samyak-jain/agora_backend/pkg/graph"
	"github.com/vektah/gqlparser/v2/ast"
)

// Config holds the GraphQL configuration
type Config struct {
	Resolvers *graph.Resolver
}

// NewExecutableSchema creates a new executable schema
func NewExecutableSchema(cfg Config) graphql.ExecutableSchema {
	return &executableSchema{
		resolvers: cfg.Resolvers,
	}
}

type executableSchema struct {
	resolvers *graph.Resolver
}

func (e *executableSchema) Schema() *ast.Schema {
	return &ast.Schema{}
}

func (e *executableSchema) Complexity(typeName, field string, childComplexity int, args map[string]interface{}) (int, bool) {
	return 0, false
}

func (e *executableSchema) Exec(ctx context.Context) graphql.ResponseHandler {
	return func(ctx context.Context) *graphql.Response {
		return &graphql.Response{
			Data: []byte(`{"message": "GraphQL not available in stub mode"}`),
		}
	}
}
