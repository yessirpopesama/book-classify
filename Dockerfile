FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/book-distribute-server ./server

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/book-distribute-server ./book-distribute-server
COPY data/clc ./data/clc
COPY classifier_role.txt ./classifier_role.txt

EXPOSE 8080

CMD ["./book-distribute-server"]
