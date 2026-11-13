# Duas etapas com propósitos diferentes:
#   test    : roda a suíte (docker build --target test .)
#   runtime : imagem mínima só com o binário, para usar o gate em qualquer CI
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cicd ./cmd/cicd

FROM build AS test
RUN go vet ./... && go test ./...

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/cicd /usr/local/bin/cicd
COPY config /etc/cicd
ENTRYPOINT ["/usr/local/bin/cicd"]
