FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.20
RUN addgroup -S app && adduser -S app -G app
COPY --from=build /out/api /usr/local/bin/api
USER app
EXPOSE 8080
ENTRYPOINT ["api"]
