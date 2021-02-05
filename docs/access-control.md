# Access Control

Bramble has a simple (yet powerful) access control mechanism allowing plugins to define field-level permissions for incoming queries.

!> Permissions must be added to incoming queries through plugins, use the provided [JWT plugin](/plugins?id=jwt-auth) or learn [how to write a plugin](write-plugin.md).

## OperationPermissions

The [`OperationPermissions`](https://pkg.go.dev/github.com/movio/bramble/bramble#OperationPermissions) type defines which fields can be requested for a given query.

By adding an `OperationPermissions` to the query context it is possible to control the allowed fields for that query.

At every level of the schema it is possible to:

- Enable a list of white-listed subfields
  or
- Enable all subfields

### JSON Representation

`OperationPermissions` implements a custom JSON unmarshaller, so it is possible to represent the permissions with a more accessible representation.

```json
{
  "query": {
    "movies": "*",
    "cinemas": ["id", "name"]
  }
}
```

The syntax is as follow:

- `"*"`: Allows all sub-fields
- An array of fields `[field1, field2]`: Allows the specified fields and all their sub-fields

See below for more examples.

### Examples

Let's imagine we have the following schema:

```graphql
type Cast {
  firstName: String!
  lastName: String!
}

type Movie {
  id: ID!
  title: String!
  releaseYear: Int!
  cast: [Cast!]
}

type Cinema {
  id: ID!
  name: String!
  location: String!
}

type Query {
  movies: [Movie!]
  cinemas: [Cinemas!]
}
```

#### AllowAll and sub-fields

If we want to allow the user to query every field on `Movie` but only a subset for `Cinema` we could define the permissions as:

```go
OperationPermissions{
  AllowedRootQueryFields: AllowedFields{
    AllowedSubFields: map[string]AllowedFields{
      "movies": AllowedField{
        AllowAll: true,
      }
      "cinemas": AllowedField{
        AllowedSubFields: map[string]AllowedField{
          "id": AllowedField{},
          "name": AllowedField{},
        }
      }
    }
  }
}
```

This can be more easily represented with JSON:

```json
{
  "query": {
    "movies": "*",
    "cinemas": ["id", "name"]
  }
}
```

Now if we try to execute the following query:

```graphql
query {
  cinemas {
    name
    location
  }
}
```

Bramble will filter out unauthorized fields and execute

```graphql
query {
  cinemas {
    name
  }
}
```

#### Allow all

```json
{
  "query": "*",
  "mutation": "*"
}
```

will give access to every query and mutation.

#### Nested fields

If we want to allow only `movies.title` and `movies.cast.firstName` we can write something like:

```json
{
  "query": {
    "movies": {
      "title": [],
      "cast": {
        "firstName": []
      }
    }
  }
}
```

On the other hand, using the array notation

```json
{
  "query": {
    "movies": ["title", "cast"]
  }
}
```

would give access to all subfields of the named fields so that

```graphql
query {
  movies {
    title
    cast {
      firstName
      lastName
    }
  }
}
```

is valid.

### Testing the permissions

It is possible to programmatically check what the allowed schema would be for
a given set of permissions by using the `OperationPermissions.FilterSchema`
method.

<details>
<summary>Example Go code</summary>

Here is an example `PrintFilteredSchema` function thats prints the filtered schema from the source schema and JSON permissions.

```go
import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
)

func PrintFilteredSchema(schema, permissionsJSON string) {
	var perms OperationPermissions
	_ = json.Unmarshal([]byte(permissionsJSON), &perms)
	parsedSchema := gqlparser.MustLoadSchema(&ast.Source{Input: schema})

	filteredSchema := perms.FilterSchema(parsedSchema)

	fmt.Println(formatSchema(filteredSchema))
}

func formatSchema(schema *ast.Schema) string {
	buf := bytes.NewBufferString("")
	f := formatter.NewFormatter(buf)
	f.FormatSchema(schema)
	return buf.String()
}
```

</details>

## Setting permissions for incoming requests

From a plugin it is possible to add permissions to any incoming request context using `bramble.AddPermissionsToContext`.

```go
func (p *MyPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		r = r.WithContext(bramble.AddPermissionsToContext(r.Context(), permissions))
		h.ServeHTTP(rw, r)
	}
}
```

If Bramble finds permissions in a query context it will automatically filter out unauthorized fields.

If a query contains both authorized and unauthorized fields:

- unauthorized fields will be removed from the query
- an error will be added to the response
- **the query will still proceed with authorized fields**

## JWT and role based access control

Bramble provides a simple plugin for JWT and role based access control.

The permissions syntax described above can be used to configure roles (i.e. a named set of permissions).

See [JWT Auth plugin](/plugins?id=jwt-auth)

!> When using roles on a public-facing instance it is recommended to use
fine-grained whitelisting to avoid newly federated services to inadvertently
expose new fields publicly.
