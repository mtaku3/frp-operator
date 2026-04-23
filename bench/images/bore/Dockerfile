FROM rust:1.85-slim AS build
RUN apt-get update && \
    apt-get install -y --no-install-recommends build-essential && \
    rm -rf /var/lib/apt/lists/*
RUN cargo install bore-cli --version 0.5.1

FROM debian:bookworm-slim
COPY --from=build /usr/local/cargo/bin/bore /usr/local/bin/bore
ENTRYPOINT ["bore"]
