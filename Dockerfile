FROM golang:1.24-alpine as builder

WORKDIR /app
RUN apk update && apk upgrade && apk add --update alpine-sdk && \
    apk add --no-cache bash git make
COPY . .
RUN make build

# Execution stage
FROM alpine:latest
WORKDIR /app/
COPY --from=builder /app/dist/tileserver .
COPY layers.yml .
EXPOSE 8888
CMD ["./tileserver"]