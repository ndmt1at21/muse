# Multi-stage build for any of the Go services. Pass SERVICE as the package
# path, e.g. core/cmd/core, examples/bff-consumer/cmd/bff-consumer,
# examples/bff-admin/cmd/bff-admin.
# The whole repo (both go.work modules) is copied so the gamekit replace resolves.
FROM golang:1.25.7-bookworm AS build
WORKDIR /src
ENV GOTOOLCHAIN=local
COPY . .
ARG SERVICE
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/app ./${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
