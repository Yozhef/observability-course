# --- build stage ---
FROM golang:1.23-alpine AS build
WORKDIR /src

COPY go.mod ./
COPY main.go ./
# go mod tidy resolves and pins all transitive deps inside the build
RUN go mod tidy && CGO_ENABLED=0 go build -o /bin/demo-service .

# --- runtime stage ---
FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /bin/demo-service /bin/demo-service
EXPOSE 8080
ENTRYPOINT ["/bin/demo-service"]
