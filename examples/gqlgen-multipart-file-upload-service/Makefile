ARTIFACT=gqlgen-service
DEF=gqlgen.yml schema.graphql
GEN=models_gen.go generated.go

build: $(ARTIFACT)

.PHONY: clean
clean:
	rm -f $(ARTIFACT) $(GEN)

.PHONY: generate
generate: $(GEN)

$(GEN): $(DEF)
	go generate

gqlgen-service: $(GEN) $(wildcard *.go)
	go build
