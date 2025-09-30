FROM golang:1.23

ARG TAG=v1.2.0

RUN apt-get update && apt-get install -y git

RUN git clone https://github.com/primev/mev-commit.git /app && \
    cd /app && \
    git checkout ${TAG} && \
    git -c submodule."external/geth".update=none submodule update --init --recursive

WORKDIR /app/tools

RUN go mod tidy

WORKDIR /app/tools/bidder-bot

RUN go build -o bidder-bot .

CMD ["./bidder-bot"]
