# ---------- build stage ----------
FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o app ./cmd/server

# ---------- runtime stage ----------
FROM golang:1.24

WORKDIR /app
COPY --from=builder /app/app .

EXPOSE 8080
CMD ["./app"]
