FROM golang:1.23-alpine AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /bin/api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache tzdata
RUN adduser -D -H appuser
WORKDIR /app
COPY --from=build /bin/api /bin/api
USER appuser
EXPOSE 8080
CMD ["/bin/api"]
