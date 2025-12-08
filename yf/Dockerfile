# ================================
# Dockerfile 构建说明
# ================================
# 需要准备以下文件:
#   - Dockerfile（本文件）
#   - flow2ftp（Linux amd64 二进制文件）
#   - yaf.init（默认配置）
#
# 构建:
#   GOOS=linux GOARCH=amd64 go build -o flow2ftp ./cmd/flow2ftp
#   docker build -t yaf-flow2ftp:alpha1 .
# ================================


# ================================
# 1. 构建阶段：编译 libfixbuf / YAF / super_mediator
# ================================

FROM debian:bookworm-slim AS builder

ARG APT_PROXY
ENV DEBIAN_FRONTEND=noninteractive
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig

ARG FIXBUF_VERSION=3.0.0.alpha2
ARG YAF_VERSION=3.0.0.alpha4
ARG SM_VERSION=2.0.0.alpha3

RUN set -eux; \
    rm -f /etc/apt/sources.list.d/debian.sources || true; \
    echo "deb http://mirrors.aliyun.com/debian bookworm main contrib non-free non-free-firmware" > /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian bookworm-updates main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian-security bookworm-security main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        build-essential pkg-config wget curl ca-certificates \
        libglib2.0-dev libpcap-dev libpcre3-dev zlib1g-dev libssl-dev liblua5.3-dev; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /tmp

# ----------------
# 1.1 libfixbuf
# ----------------
RUN HTTP_PROXY=${APT_PROXY} HTTPS_PROXY=${APT_PROXY} \
    curl -fSL "https://tools.netsa.cert.org/releases/libfixbuf-${FIXBUF_VERSION}.tar.gz" -o libfixbuf.tar.gz && \
    tar xzf libfixbuf.tar.gz && \
    cd "libfixbuf-${FIXBUF_VERSION}" && \
    ./configure --disable-tools && make -j"$(nproc)" && make install && ldconfig && \
    cd /tmp && rm -rf "libfixbuf-${FIXBUF_VERSION}" libfixbuf.tar.gz

# ----------------
# 1.2 YAF（开启 applabel + DPI）
# ----------------
RUN HTTP_PROXY=${APT_PROXY} HTTPS_PROXY=${APT_PROXY} \
    curl -fSL "https://tools.netsa.cert.org/releases/yaf-${YAF_VERSION}.tar.gz" -o yaf.tar.gz && \
    tar xzf yaf.tar.gz && \
    cd "yaf-${YAF_VERSION}" && \
    ./configure --enable-applabel --enable-dpi && \
    make -j"$(nproc)" && make install && ldconfig && \
    cd /tmp && rm -rf "yaf-${YAF_VERSION}" yaf.tar.gz

# ----------------
# 1.3 super_mediator
# ----------------
RUN HTTP_PROXY=${APT_PROXY} HTTPS_PROXY=${APT_PROXY} \
    curl -fSL "https://tools.netsa.cert.org/releases/super_mediator-${SM_VERSION}.tar.gz" -o sm.tar.gz && \
    tar xzf sm.tar.gz && \
    cd "super_mediator-${SM_VERSION}" && \
    ./configure --with-mysql=no && \
    make -j"$(nproc)" && make install && ldconfig && \
    cd /tmp && rm -rf "super_mediator-${SM_VERSION}" sm.tar.gz



# ================================
# 2. 运行阶段
# ================================

FROM debian:bookworm-slim AS runtime
ENV DEBIAN_FRONTEND=noninteractive

RUN set -eux; \
    rm -f /etc/apt/sources.list.d/debian.sources || true; \
    echo "deb http://mirrors.aliyun.com/debian bookworm main contrib non-free non-free-firmware" > /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian-updates main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian-security bookworm-security main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates libglib2.0-0 libpcap0.8 libpcre3 zlib1g libssl3 liblua5.3-0 \
        bash procps tcpdump netcat-traditional iproute2 net-tools dnsutils psmisc vim-tiny less \
        supervisor tzdata; \
    ln -fs /usr/share/zoneinfo/Asia/Shanghai /etc/localtime; \
    echo "Asia/Shanghai" > /etc/timezone; dpkg-reconfigure -f noninteractive tzdata; \
    rm -rf /var/lib/apt/lists/*

ENV TZ=Asia/Shanghai

COPY --from=builder /usr/local/ /usr/local/

RUN echo "/usr/local/lib"  >  /etc/ld.so.conf.d/usr-local.conf; \
    echo "/usr/local/lib64" >> /etc/ld.so.conf.d/usr-local.conf; \
    ldconfig

ENV LD_LIBRARY_PATH=/usr/local/lib:/usr/local/lib64

# flow2ftp 二进制
COPY flow2ftp /usr/local/bin/flow2ftp
RUN chmod +x /usr/local/bin/flow2ftp

# /data 目录（可挂载，也可不挂载）
RUN mkdir -p /data && chown -R yaf:yaf /data

# /etc/yaf 配置目录
RUN mkdir -p /etc/yaf

# supervisor 目录
RUN mkdir -p /var/log/supervisor /var/run/supervisor


# ====== 默认配置目录 + 拷贝 yaf.init ======
RUN mkdir -p /opt/yaf
COPY yaf.init /opt/yaf/yaf.init
RUN ln -sf /opt/yaf/yaf.init /etc/yaf/yaf.init
RUN chown -R root:root /opt/yaf /etc/yaf


# 非 root 用户运行
RUN useradd -r -s /usr/sbin/nologin yaf && \
    chown -R yaf:yaf /opt/yaf /var/log/supervisor /var/run/supervisor /data



# ================================
# 启动脚本
# ================================

RUN cat >/usr/local/bin/start_yaf.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

YAF_CONFIG_FILE="${YAF_CONFIG_FILE:-/etc/yaf/yaf.init}"

echo "[start_yaf] 使用 YAF 配置文件: ${YAF_CONFIG_FILE}"
if [ ! -f "${YAF_CONFIG_FILE}" ]; then
  echo "[start_yaf] ERROR: 未找到 ${YAF_CONFIG_FILE}" >&2
  exit 1
fi

exec yaf --config "${YAF_CONFIG_FILE}" "$@"
EOF

RUN cat >/usr/local/bin/start_pipeline.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

YAF_CONFIG_FILE="${YAF_CONFIG_FILE:-/etc/yaf/yaf.init}"
SM_LISTEN_PORT="${SM_LISTEN_PORT:-18000}"
SM_FIELDS="${SM_FIELDS:-flowStartMilliseconds,flowEndMilliseconds,sourceIPv4Address,destinationIPv4Address,sourceTransportPort,destinationTransportPort,protocolIdentifier,silkAppLabel}"

echo "[start_pipeline] super_mediator 监听端口: ${SM_LISTEN_PORT}"
echo "[start_pipeline] TEXT 输出字段: ${SM_FIELDS}"

exec bash -c "
  super_mediator \
    --ipfix-input=tcp \
    --ipfix-port=\"${SM_LISTEN_PORT}\" \
    --output-mode=TEXT \
    --print-headers \
    --out=- \
    --fields=\"${SM_FIELDS}\" \
    localhost \
  | flow2ftp -config \"${YAF_CONFIG_FILE}\" -data-dir /data
"
EOF

RUN chmod +x /usr/local/bin/start_yaf.sh /usr/local/bin/start_pipeline.sh



# ================================
# supervisor 配置
# ================================

RUN cat >/etc/supervisor/supervisord.conf <<'EOF'
[unix_http_server]
file=/var/run/supervisor.sock

[supervisord]
nodaemon=true
logfile=/var/log/supervisor/supervisord.log
logfile_maxbytes=50MB
logfile_backups=5
pidfile=/var/run/supervisord.pid

[program:yaf]
command=/usr/local/bin/start_yaf.sh
autorestart=true
startsecs=3

stdout_logfile=/var/log/supervisor/supervisord.log
stderr_logfile=/var/log/supervisor/supervisord.log

[program:pipeline]
command=/usr/local/bin/start_pipeline.sh
autorestart=true
startsecs=3

stdout_logfile=/var/log/supervisor/supervisord.log
stderr_logfile=/var/log/supervisor/supervisord.log
EOF



# ================================
# 最终运行用户 + 目录
# ================================

USER yaf
WORKDIR /opt/yaf

ENTRYPOINT ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]
CMD []
