# Getting started

## Preparing your services for federation

For a service to be federated by Bramble the only requirement is to implement the `Service` type and query:

```graphql
type Service {
  name: String! # unique name for the service
  version: String! # any string
  schema: String! # the full schema for the service
}

type Query {
  service: Service!
}
```

!> The `Service` type is only used internally by Bramble and will not be part of the exposed schema.

## Configuration

Create a JSON config file with the following format:

_config.json_

```json
{
  "services": ["http://service1/query", "http://service2/query"] // list of services to federate
}
```

For the full list of available options see [configuration](configuration.md).

## Running Bramble

### Install locally

(requires Golang)

```
go get https://github.com/movio/bramble
go install https://github.com/movio/bramble
```

```
bramble config.json
```

### Docker

```
docker run -p 8082:8082 -p 8083:8083 -p 8084:8084 -v $(PWD)/config.json:/config.json ghcr.io/movio/bramble
```

## Querying Bramble

Bramble can be queried like any GraphQL service, just point your favourite
client to `http://localhost:8082/query`.
