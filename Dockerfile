FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /dudle-ng .
RUN mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /dudle-ng /dudle-ng
# create /data owned by the non-root user so a fresh volume is writable
COPY --from=build --chown=65532:65532 /data /data
ENV DUDLE_ADDR=:8080 DUDLE_DB=/data/dudle.db
VOLUME /data
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/dudle-ng"]
