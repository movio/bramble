# Minimal Example
Via `docker-compose` we can simulate an environment with three services all contributing to the boundary type `Foo`.

```
docker-compose up
```

The gateway will then be hosted on `http://localhost:8082/query`, be sure to point a GraphQL client to this address.

```graphql
{
  randomFoo {
    nodejs
    graphGophers
    gqlgen
  }
}
```

_Note: each field is resolved by each different service here._
