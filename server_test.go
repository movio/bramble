package bramble

import (
	"context"
	"testing"

	testsrv1 "github.com/movio/bramble/test_srv_boundary_fragment/service1"
	testsrv2 "github.com/movio/bramble/test_srv_boundary_fragment/service2"
	"github.com/movio/bramble/testsrv"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
)

func TestFederatedQuery(t *testing.T) {
	gizmoService := testsrv.NewGizmoService()
	gadgetService := testsrv.NewGadgetService()

	executableSchema := NewExecutableSchema(nil, 10, nil, NewService(gizmoService.URL), NewService(gadgetService.URL))

	require.NoError(t, executableSchema.UpdateSchema(context.TODO(), true))

	query := gqlparser.MustLoadQuery(executableSchema.MergedSchema, `{
		gizmo(id: "GIZMO1") {
			id
			name
		}
	}`)

	ctx := testContextWithoutVariables(query.Operations[0])

	response := executableSchema.ExecuteQuery(ctx)
	expectedResponse := `
	{
		"gizmo": {
			"id": "GIZMO1",
			"name": "Gizmo #1"
		}
	}
	`
	jsonEqWithOrder(t, expectedResponse, string(response.Data))
	gizmoService.Close()
	gadgetService.Close()
}

func TestFederatedQueryWithMultipleFragmentSpreads(t *testing.T) {
	gizmoService := testsrv.NewGizmoService()
	gadgetService := testsrv.NewGadgetService()

	executableSchema := NewExecutableSchema(nil, 10, nil, NewService(gizmoService.URL), NewService(gadgetService.URL))

	require.NoError(t, executableSchema.UpdateSchema(context.TODO(), true))

	t.Run("first fragment matches", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(executableSchema.MergedSchema, `{
			gizmo(id: "GIZMO1") {
				id
				name
				gadget {
					id
					name
					... on Jetpack {
						range
					}
					... on InvisibleCar {
						cloaked
					}
				}
			}
		}`)

		ctx := testContextWithoutVariables(query.Operations[0])

		response := executableSchema.ExecuteQuery(ctx)
		expectedResponse := `
		{
			"gizmo": {
				"id": "GIZMO1",
				"name": "Gizmo #1",
				"gadget": {
					"id": "JETPACK1",
					"name": "Jetpack #1",
					"range": "500km"
				}
			}
		}`

		jsonEqWithOrder(t, expectedResponse, string(response.Data))
	})

	t.Run("second fragment matches", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(executableSchema.MergedSchema, `{
			gizmo(id: "GIZMO2") {
				id
				name
				gadget {
					id
					name
					... on Jetpack {
						range
					}
					... on InvisibleCar {
						cloaked
					}
				}
			}
		}`)

		ctx := testContextWithoutVariables(query.Operations[0])

		response := executableSchema.ExecuteQuery(ctx)
		expectedResponse := `
		{
			"gizmo": {
				"id": "GIZMO2",
				"name": "Gizmo #2",
				"gadget": {
					"id": "AM1",
					"name": "Vanquish",
					"cloaked": true
				}
			}
		}`

		jsonEqWithOrder(t, expectedResponse, string(response.Data))
	})

	t.Run("no fragments match", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(executableSchema.MergedSchema, `{
			gizmo(id: "GIZMO2") {
				id
				name
				gadget {
					id
					name
					... on Jetpack {
						range
					}
				}
			}
		}`)

		ctx := testContextWithoutVariables(query.Operations[0])

		response := executableSchema.ExecuteQuery(ctx)
		expectedResponse := `
		{
			"gizmo": {
				"id": "GIZMO2",
				"name": "Gizmo #2",
				"gadget": {
					"id": "AM1",
					"name": "Vanquish"
				}
			}
		}`

		jsonEqWithOrder(t, expectedResponse, string(response.Data))
	})

	gizmoService.Close()
	gadgetService.Close()

}

func TestFederatedQueryWithBoundary(t *testing.T) {
	gizmoService := testsrv1.NewGizmoService()
	gadgetService := testsrv2.NewGadgetService()

	executableSchema := NewExecutableSchema(nil, 10, nil, NewService(gizmoService.URL), NewService(gadgetService.URL))

	require.NoError(t, executableSchema.UpdateSchema(context.TODO(), true))

	t.Run("It should resolve boundary fields on fragments correctly", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(executableSchema.MergedSchema, `{
			gizmo(id: "GIZMO1") {
				id
				gadget {
					... on Jetpack {
						name	
						range
						description
						__typename
					}
					... on InvisibleCar {
						name
						cloaked
						performance
						__typename
					}
				}
			}
		}`)

		ctx := testContextWithoutVariables(query.Operations[0])

		response := executableSchema.ExecuteQuery(ctx)
		expectedResponse := `
		{
			"gizmo": {
				"id": "GIZMO1",
				"gadget": {
					"name": "Jetpack #1",
					"range": "500km",
					"description":"Jetpack #1 Description",
					"__typename": "Jetpack"
				}
			}
		}`

		// Assert that response.Errors is empty
		require.Empty(t, response.Errors)
		jsonEqWithOrder(t, expectedResponse, string(response.Data))
	})

	gizmoService.Close()
	gadgetService.Close()

}
