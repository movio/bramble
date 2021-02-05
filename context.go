package bramble

import (
	"context"
	"net/http"
)

type contextKey string
type brambleContextKey int

const permissionsContextKey brambleContextKey = 1
const requestHeaderContextKey brambleContextKey = 2

// AddPermissionsToContext adds permissions to the request context. If
// permissions are set the execution will check them against the query.
func AddPermissionsToContext(ctx context.Context, perms OperationPermissions) context.Context {
	return context.WithValue(ctx, permissionsContextKey, perms)
}

// GetPermissionsFromContext returns the permissions stored in the context
func GetPermissionsFromContext(ctx context.Context) (OperationPermissions, bool) {
	v := ctx.Value(permissionsContextKey)
	if v == nil {
		return OperationPermissions{}, false
	}

	if perm, ok := v.(OperationPermissions); ok {
		return perm, true
	}

	return OperationPermissions{}, false
}

// AddOutgoingRequestsHeaderToContext adds a header to all outgoings requests for the current query
func AddOutgoingRequestsHeaderToContext(ctx context.Context, key, value string) context.Context {
	h, ok := ctx.Value(requestHeaderContextKey).(http.Header)
	if !ok {
		h = make(http.Header)
	}
	h.Add(key, value)

	return context.WithValue(ctx, requestHeaderContextKey, h)
}

// GetOutgoingRequestHeadersFromContext get the headers that should be added to outgoing requests
func GetOutgoingRequestHeadersFromContext(ctx context.Context) http.Header {
	h, _ := ctx.Value(requestHeaderContextKey).(http.Header)
	return h
}
