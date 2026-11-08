FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates curl

ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=off

WORKDIR /app

COPY go.mod ./
COPY . .

RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /app/server ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl tzdata && \
    curl -fsSL -o /usr/local/share/ca-certificates/russian_trusted_root_ca.crt \
      https://gu-st.ru/content/lending/russian_trusted_root_ca_pem.crt || true && \
    if [ -f /usr/local/share/ca-certificates/russian_trusted_root_ca.crt ]; then \
      cp /usr/local/share/ca-certificates/russian_trusted_root_ca.crt /etc/ssl/certs/russian_trusted_root_ca.pem && \
      update-ca-certificates; \
    fi

WORKDIR /app

COPY --from=builder /app/server /app/server

EXPOSE 8080

CMD ["/app/server"]

