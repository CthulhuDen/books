FROM golang:alpine as build

RUN go install github.com/pressly/goose/v3/cmd/goose@latest

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /crawler ./cmd/crawler

FROM alpine

COPY --from=build /go/bin/goose /
COPY --from=build /crawler /
COPY db /db

#EXPOSE 8080

#CMD ["/crawler"] # require explicit command, at least crawler shouldn't be default
