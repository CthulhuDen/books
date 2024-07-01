FROM golang:alpine AS build

RUN go install github.com/pressly/goose/v3/cmd/goose@latest

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /crawler ./cmd/crawler
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

FROM alpine

EXPOSE 8080
CMD ["/server"]

COPY --from=build /go/bin/goose /
COPY --from=build /crawler /
COPY --from=build /server /

COPY db /db
COPY web /web
COPY openapi.yaml web/

RUN ls -la /web && exit 1
