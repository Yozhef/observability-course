# --- build stage: builds all three service binaries ---
FROM golang:1.23-alpine AS build
WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
# go mod tidy resolves and pins all transitive deps inside the build
RUN go mod tidy && \
    CGO_ENABLED=0 go build -o /bin/checkout ./cmd/checkout && \
    CGO_ENABLED=0 go build -o /bin/payment ./cmd/payment && \
    CGO_ENABLED=0 go build -o /bin/worker ./cmd/worker

# --- runtime stage ---
FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /bin/checkout /bin/payment /bin/worker /bin/
EXPOSE 8080 8081 8083
# docker-compose chooses which binary to run per service
ENTRYPOINT ["/bin/checkout"]
