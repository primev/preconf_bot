FROM golang:1.23 AS builder

# Commit from main https://github.com/primev/mev-commit/commit/5bfb48c6612fd2ff32e0ddef6ffbc782ec4ccc86
ARG COMMIT=5bfb48c6612fd2ff32e0ddef6ffbc782ec4ccc86

RUN apt-get update && \
    apt-get install -y git make curl jq

WORKDIR /build

RUN git clone https://github.com/primev/mev-commit.git . && \
    git checkout ${COMMIT} && \
    git -c submodule."external/geth".update=none submodule update --init --recursive

WORKDIR /build/p2p

RUN make build

FROM golang:1.23

RUN apt-get update && \
    apt-get install -y curl jq && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /build/p2p/bin/mev-commit /usr/local/bin/mev-commit
RUN chmod +x /usr/local/bin/mev-commit

WORKDIR /app
COPY entrypoint.sh .
RUN chmod +x entrypoint.sh

CMD ["./entrypoint.sh"]
