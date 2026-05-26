FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o togos .

FROM scratch
COPY --from=build /app/togos /togos
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/togos", "-data-dir", "/data"]
