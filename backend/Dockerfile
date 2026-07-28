FROM golang:1.23-alpine AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM build AS api-build
RUN go build -o /bin/api ./cmd/api

FROM build AS worker-build
RUN go build -o /bin/worker ./cmd/worker

FROM build AS sample-data-build
RUN go build -o /bin/sample-data ./cmd/sample-data

FROM alpine:3.20 AS runtime
RUN apk add --no-cache tzdata
RUN adduser -D -H appuser
WORKDIR /app

FROM runtime AS api
COPY --from=api-build /bin/api /bin/api
COPY --from=sample-data-build /bin/sample-data /bin/sample-data
USER appuser
EXPOSE 8080
CMD ["/bin/api"]

FROM runtime AS worker
COPY --from=worker-build /bin/worker /bin/worker
USER appuser
CMD ["/bin/worker"]
