# Build Golang binary
FROM golang:1.26.10 AS build-golang

WORKDIR /go/src/github.com/gophish/gophish
COPY . .
# Rebuild the JavaScript and CSS bundles before compiling, since they're
# embedded in the binary.
RUN go mod download && go run ./tools/assets && go build -v


# Runtime container
FROM debian:stable-slim

RUN useradd -m -d /opt/gophish -s /bin/bash app

RUN apt-get update && \
	apt-get install --no-install-recommends -y jq libcap2-bin ca-certificates && \
	apt-get clean && \
	rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

WORKDIR /opt/gophish
# Templates, admin assets, and migrations are embedded in the binary. Only the
# files Gophish reads from disk at runtime are copied alongside it.
COPY --from=build-golang /go/src/github.com/gophish/gophish/gophish ./
COPY --from=build-golang /go/src/github.com/gophish/gophish/config.json ./
COPY --from=build-golang /go/src/github.com/gophish/gophish/docker/ ./docker/
COPY --from=build-golang /go/src/github.com/gophish/gophish/static/db/ ./static/db/
COPY --from=build-golang /go/src/github.com/gophish/gophish/static/endpoint/ ./static/endpoint/
RUN chown app. config.json

RUN setcap 'cap_net_bind_service=+ep' /opt/gophish/gophish

USER app
RUN sed -i 's/127.0.0.1/0.0.0.0/g' config.json
RUN touch config.json.tmp

EXPOSE 3333 8080 8443 80

CMD ["./docker/run.sh"]
