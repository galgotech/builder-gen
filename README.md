# buider-gen

go run examples/builder-gen/main.go \
  -v 10 \
  --go-header-file ./boilerplate/no-boilerplate.go.txt \
  --input-dirs ./examples/builder-gen/test/ \
  --output-base ./
