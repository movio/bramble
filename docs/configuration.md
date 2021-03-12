# Configuration

Bramble can be configured by passing one or more JSON config file with the `-conf` parameter.

Config files are also hot-reloaded on change (see below for list of supported options).

Sample configuration:

```json
{
  "services": ["http://service1/query", "http://service2/query"],
  "gateway-port": 8082,
  "private-port": 8083,
  "metrics-port": 8084,
  "log-level": "info",
  "poll-interval": "5s",
  "max-requests-per-query": 50,
  "max-client-response-size": 1048576,
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

- `gateway-port`: public port for the gateway, this is where the query endpoint
  is exposed. Plugins can expose additional endpoints on this port.

  - Default: 8082
  - Supports hot-reload: No

- `private-port`: A port for plugins to expose private endpoints. Not used by default.

  - Default: 8083
  - Supports hot-reload: No

- `metrics-port`: Port used to expose Prometheus metrics.

  - Default: 8084
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

- `plugins`: Optional list of plugins to enable. See [plugins](plugins.md) for plugins-specific config.

  - Supports hot-reload: Partial. `Configure` method of previously enabled plugins will get called with new configuration.

- `extensions`: Non-standard configuration, can be used to share configuration across plugins.
