FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      wireguard-tools iptables iputils-ping dnsutils procps iproute2 && \
    rm -rf /var/lib/apt/lists/*
