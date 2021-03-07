FROM golang:1.11-alpine as builder
RUN apk add make

WORKDIR /go/src/github.com/kelda-inc/kelda

ADD vendor vendor
RUN CGO_ENABLED=0 go install -i ./vendor/...

ADD . .
ARG KELDA_VERSION
RUN make

FROM alpine

# Add certificates needed to make HTTPS requests to S3.
RUN apk add --no-cache ca-certificates

COPY --from=builder /go/src/github.com/kelda-inc/kelda/kelda /bin/kelda
CMD ["kelda", "minion"]
