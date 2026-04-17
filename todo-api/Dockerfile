# syntax=docker/dockerfile:1

FROM golang:1.22 AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /out/todo-api ./

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /out/todo-api /todo-api
EXPOSE 9090
USER nonroot:nonroot
ENTRYPOINT ["/todo-api"]
