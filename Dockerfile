# -=-=-=-=-=-=- Compile Image -=-=-=-=-=-=-

FROM golang:1 AS stage-compile

WORKDIR /go/src/app
COPY . .

# hadolint ignore=DL3062
RUN go get -d -v ./... && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/rescuestream-dji-bridge ./cmd/rescuestream-dji-bridge && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/mock-rcplus ./cmd/mock-rcplus

# -=-=-=-=- Final Distroless Image -=-=-=-=-

# hadolint ignore=DL3007
FROM gcr.io/distroless/static-debian12:latest AS stage-final

COPY --from=stage-compile /out/rescuestream-dji-bridge /out/mock-rcplus /
CMD ["/rescuestream-dji-bridge"]
