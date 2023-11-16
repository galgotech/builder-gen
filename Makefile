addheaders:
	@command -v addlicense > /dev/null || go install -modfile=tools.mod -v github.com/google/addlicense
	@addlicense -c "The builder-gen Authors" -l apache .

fmt:
	@go vet ./...
	@go fmt ./...

lint:
	make addheaders
	make fmt

.PHONY: test
test:
	make lint
	@go test ./...
