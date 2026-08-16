FROM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG BUILD_VERSION=devel
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.buildVersion=${BUILD_VERSION}" \
    -o /out/chain-api ./cmd/chain-api

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35

COPY --from=build /out/chain-api /chain-api

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/chain-api"]
