FROM golang:1.21

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the binary as preconf_bot from main.go at the top level
RUN go build -o preconf_bot .

ENTRYPOINT ["./preconf_bot"]