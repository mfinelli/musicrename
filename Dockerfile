FROM golang:alpine AS source
WORKDIR /musicrename
COPY . /musicrename
RUN go mod vendor

FROM source AS build
RUN apk add gcc grep libc-dev make scdoc
RUN make

FROM build AS test
RUN apk add ffmpeg
RUN go test ./...

FROM alpine

LABEL org.opencontainers.image.title=musicrename
LABEL org.opencontainers.image.version=v3.1.1
LABEL org.opencontainers.image.description="command line music management"
LABEL org.opencontainers.image.url=https://github.com/mfinelli/musicrename
LABEL org.opencontainers.image.source=https://github.com/mfinelli/musicrename
LABEL org.opencontainers.image.licenses=GPL-3.0-or-later

RUN addgroup -S musicrename && adduser -S musicrename -G musicrename
COPY --from=source /musicrename /usr/src/musicrename
COPY --from=build /musicrename/mrr /usr/bin/mrr
USER musicrename
CMD ["mrr"]
