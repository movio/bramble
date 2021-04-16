package bramble

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextOutgoingRequestHeaders(t *testing.T) {
	ctx := context.Background()
	ctx = AddOutgoingRequestsHeaderToContext(ctx, "My-Header-1", "value1")
	ctx = AddOutgoingRequestsHeaderToContext(ctx, "My-Header-1", "value2")
	ctx = AddOutgoingRequestsHeaderToContext(ctx, "My-Header-2", "value3")

	header := GetOutgoingRequestHeadersFromContext(ctx)
	assert.Equal(t, http.Header{"My-Header-1": []string{"value1", "value2"},
		"My-Header-2": []string{"value3"},
	}, header)
}
