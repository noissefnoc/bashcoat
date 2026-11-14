# syntax=docker/dockerfile:1

ARG GO_IMAGE=golang:1.27-bookworm
FROM ${GO_IMAGE} AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /out/bashcoat ./cmd/bashcoat

FROM ubuntu:26.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    bats \
    build-essential \
    ca-certificates \
    coreutils \
    git \
    kcov \
    python3 \
    ruby \
    ruby-dev \
  && rm -rf /var/lib/apt/lists/*

RUN gem install --no-document bashcov simplecov simplecov-lcov

COPY --from=builder /out/bashcoat /usr/local/bin/bashcoat

WORKDIR /workspace
