package graphql

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Long preserves 64-bit byte counts and Unix timestamps in the GraphQL schema.
type Long int64

func (Long) ImplementsGraphQLType(name string) bool { return name == "Long" }

func (value *Long) UnmarshalGraphQL(input any) error {
	switch input := input.(type) {
	case int32:
		*value = Long(input)
	case int64:
		*value = Long(input)
	case float64:
		*value = Long(input)
	case string:
		parsed, err := strconv.ParseInt(input, 10, 64)
		if err != nil {
			return err
		}
		*value = Long(parsed)
	default:
		return fmt.Errorf("Long cannot represent %T", input)
	}
	return nil
}

func (value Long) MarshalJSON() ([]byte, error) { return json.Marshal(int64(value)) }
