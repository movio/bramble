# Debugging

## Debug headers

If the `X-Bramble-Debug` header is present Bramble will add the requested debug information to the response `extensions`.
One or multiple of the following options can be provided (white space separated):

- `variables`: input variables
- `query`: input query
- `plan`: the query plan, including services and subqueries
- `timing`: total execution time for the query (as a duration string, e.g. `12ms`)
- `all` (all of the above)
