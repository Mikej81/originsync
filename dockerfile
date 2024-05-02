FROM golang:1.18 as builder

# Enable Go modules support and disable CGO
ENV GO111MODULE=on \
    CGO_ENABLED=0

WORKDIR /app

# Copy the go mod and sum files first to leverage Docker cache layering
COPY go.mod go.sum ./

RUN go mod download

COPY *.go .
COPY *.sum .

RUN go build -o originsync -v .

FROM alpine:latest  

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/originsync .

EXPOSE 8080

CMD ["./originsync"]
