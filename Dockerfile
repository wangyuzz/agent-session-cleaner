# syntax=docker/dockerfile:1
#
# Builds the Linux binary, for whichever architecture the image is built for:
#
#   docker buildx build --platform linux/amd64 --output type=local,dest=dist .
#   docker buildx build --platform linux/arm64 --output type=local,dest=dist .
#
# Bullseye rather than a current Debian on purpose. The glibc a binary is
# linked against sets the floor for the distributions it can run on, and 2.31
# reaches back to Ubuntu 20.04 and Debian 11 — building on anything newer would
# quietly cut those users off.
FROM debian:bullseye-slim AS build

# ca-certificates so uv can fetch its Python; binutils because PyInstaller
# inspects the objects it is about to bundle.
RUN apt-get update \
    && apt-get install -y --no-install-recommends binutils ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:0.11 /uv /usr/local/bin/uv

WORKDIR /src
COPY . .
RUN ./scripts/build.sh

# A stage holding nothing but the binary, so `--output type=local` writes the
# release asset and not a filesystem around it.
FROM scratch AS export
COPY --from=build /src/dist/ /
