MAINTAINER uplink coherent solutions development@uplink.at

FROM golang:latest as builder
ADD . /app
WORKDIR /app
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o /parallelping .

FROM alpine:latest
RUN apk update && apk add --no-cache ca-certificates bash util-linux procps iputils net-tools lsof curl socat
COPY --from=builder /parallelping ./
RUN chmod +x ./parallelping
