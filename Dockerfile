FROM golang:1.26-alpine AS builder

WORKDIR /app
RUN apk update && apk upgrade && apk add --update alpine-sdk && \
    apk add --no-cache bash git go-task-task
COPY . .
RUN task build

FROM scratch
WORKDIR /app/
COPY --from=builder /app/dist/tileserver .
COPY layers.yml .
EXPOSE 8888
CMD ["./tileserver"]