# buider-gen

go run main.go \
  -v 10 \
  --go-header-file ./boilerplate/no-boilerplate.go.txt \
  --input-dirs ./test/ \
  --output-base ./ \
  -O zz_generated.buildergen