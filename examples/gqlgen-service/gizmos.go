package main

import (
	"strconv"

	"github.com/go-faker/faker/v4"
)

func generateGizmos() map[string]*Gizmo {
	var gizmos = map[string]*Gizmo{}
	ids, _ := faker.RandomInt(100, 999, 10)
	for _, id := range ids {
		idstr := strconv.Itoa(id)
		gizmos[idstr] = &Gizmo{
			ID:   idstr,
			Name: faker.Name(),
		}
	}
	return gizmos
}
