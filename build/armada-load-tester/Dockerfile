FROM alpine:3.21.3

RUN addgroup -S -g 2000 armada && adduser -S -u 1000 armada -G armada

USER armada

COPY ./armada-load-tester /app/

WORKDIR /app

ENTRYPOINT ["./armada-load-tester"]
