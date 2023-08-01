FROM golang:1.19-alpine as builder
RUN apk add make

RUN mkdir kelda
WORKDIR /kelda

ADD . .
ARG KELDA_VERSION
RUN make
RUN ls -la

FROM alpine

# Add certificates needed to make HTTPS requests to S3.
RUN apk add --no-cache ca-certificates

COPY --from=builder /kelda/kelda-v1 /bin/kelda
CMD ["kelda", "minion"]
