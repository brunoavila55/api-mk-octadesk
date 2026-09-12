FROM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN go test ./...
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -o /out/healthcheck ./cmd/healthcheck

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/api /api
COPY --from=build /out/healthcheck /healthcheck
USER 65534:65534
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/healthcheck"]
ENTRYPOINT ["/api"]
