FROM golang:1.26-alpine as builder

WORKDIR /app
RUN apk update && apk upgrade && apk add --update alpine-sdk && \
    apk add --no-cache bash git go-task-task
COPY . .
RUN make build

# Execution stage
FROM scratch
WORKDIR /app/
COPY --from=builder /app/dist/tileserver .
COPY layers.yml .
EXPOSE 8888
CMD ["./tileserver"]