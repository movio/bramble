# Configuration

Bramble can be configured by passing one or more JSON config file with the `-config` parameter.

Config files are also hot-reloaded on change (see below for list of supported options).

Sample configuration:

```json
{
  "services": ["http://service1/query", "http://service2/query"],
  "gateway-port": 8082,
  "private-port": 8083,
  "metrics-port": 9009,
  "log-level": "info",
  "poll-interval": "5s",
  "max-requests-per-query": 50,
  "max-client-response-size": 1048576,
  "id-field-name": "id",
  "telemetry": {
    "enabled": true,
    "insecure": false,
    "endpoint": "http://localhost:4317",
    "serviceName": "bramble"
  },
  "plugins": [
    {
      "name": "admin-ui"
    },
    {
      "name": "my-plugin",
      "config": {
          ...
      }
    }
  ],
  "extensions": {
      ...
  }
}
```

- `services`: URLs of services to federate.

  - **Required**
  - Supports hot-reload: Yes
  - Configurable also by `BRAMBLE_SERVICE_LIST` environment variable set to a space separated list of urls which will be appended to the list

- `gateway-port`: public port for the gateway, this is where the query endpoint
  is exposed. Plugins can expose additional endpoints on this port.

  - Default: 8082
  - Supports hot-reload: No

- `private-port`: A port for plugins to expose private endpoints. Not used by default.

  - Default: 8083
  - Supports hot-reload: No

- `metrics-port`: Port used to expose Prometheus metrics.

  - Default: 9009
  - Supports hot-reload: No

- `log-level`: Log level, one of `debug`|`info`|`error`|`fatal`.

  - Default: `debug`
  - Supports hot-reload: Yes

- `poll-interval`: Interval at which federated services are polled (`service` query is called).

  - Default: `5s`
  - Supports hot-reload: No

- `max-requests-per-query`: Maximum number of requests to federated services
  a single query to Bramble can generate. For example, a query requesting
  fields from two different services might generate two or more requests to
  federated services.

  - Default: 50
  - Supports hot-reload: No

- `max-service-response-size`: The max response size that Bramble can receive from federated services

  - Default: 1MB
  - Supports hot-reload: No

- `id-field-name`: Optional customisation of the field name used to cross-reference boundary types.

  - Default: `id`
  - Supports hot-reload: No

- `telemetry`: OpenTelemetry configuration.
  - `enabled`: Enable OpenTelemetry.
    - Default: `false`
    - Supports hot-reload: No
  - `insecure`: Whether to use insecure connection to OpenTelemetry collector.
    - Default: `false`
    - Supports hot-reload: No
  - `endpoint`: OpenTelemetry collector endpoint.
    - Default: If no endpoint is specified, telemetry is disabled. Bramble will check for `BRAMBLE_OTEL_ENDPOINT` environment variable and use it if set.
    - Supports hot-reload: No
  - `serviceName`: Service name to use for OpenTelemetry.
    - Default: `bramble`
    - Supports hot-reload: No


- `plugins`: Optional list of plugins to enable. See [plugins](plugins.md) for plugins-specific config.

  - Supports hot-reload: Partial. `Configure` method of previously enabled plugins will get called with new configuration.

- `extensions`: Non-standard configuration, can be used to share configuration across plugins.
