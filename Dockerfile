FROM golang:latest as builder
ADD . /app
WORKDIR /app
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o /parallelping .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /parallelping ./
RUN chmod +x ./parallelping
