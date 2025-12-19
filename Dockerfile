# ================================
# Dockerfile 构建说明
# ================================
# 多平台构建支持（amd64 / arm64）
# 
# 构建方式（以 yf 为根目录）:
#
# 单平台构建:
#   cd yf  # 进入 yf 目录（包含 yaf-ftp/ 子目录）
#   docker build -t yf:3.3 .
#
# 多平台构建:
#   cd yf
#   docker buildx build --platform linux/amd64,linux/arm64 -t yf:3.3 .
#
# 注意：构建上下文必须在 yf 目录，因为需要访问 yaf-ftp/ 子目录和 yaf.init 文件
# ================================


# ================================
# 1. Go 编译阶段：编译 flow2ftp 二进制
# ================================

FROM golang:1.21-alpine AS go-builder

# 多平台参数（优先使用 TARGETOS/TARGETARCH，更稳定）
ARG TARGETOS
ARG TARGETARCH
ARG TARGETPLATFORM
ARG BUILDPLATFORM

WORKDIR /build

# 复制 Go 源代码
# 注意：构建上下文在 yf 目录，yaf-ftp 是子目录
COPY yaf-ftp/go.mod yaf-ftp/go.sum ./
COPY yaf-ftp/cmd ./cmd
COPY yaf-ftp/internal ./internal

# 下载依赖
RUN go mod download

# 根据 TARGETOS/TARGETARCH 或 TARGETPLATFORM 解析 GOOS 和 GOARCH
# 优先使用 TARGETOS/TARGETARCH（docker buildx 自动提供）
RUN set -eux; \
    if [ -n "${TARGETOS}" ] && [ -n "${TARGETARCH}" ]; then \
        export GOOS="${TARGETOS}" GOARCH="${TARGETARCH}"; \
        echo "Building for ${GOOS}/${GOARCH} (from TARGETOS/TARGETARCH)"; \
    elif [ -n "${TARGETPLATFORM}" ]; then \
        case ${TARGETPLATFORM} in \
            "linux/amd64") \
                export GOOS=linux GOARCH=amd64 ;; \
            "linux/arm64") \
                export GOOS=linux GOARCH=arm64 ;; \
            *) \
                echo "Unsupported platform: ${TARGETPLATFORM}" && exit 1 ;; \
        esac && \
        echo "Building for ${GOOS}/${GOARCH} (from TARGETPLATFORM)"; \
    else \
        echo "ERROR: Neither TARGETOS/TARGETARCH nor TARGETPLATFORM is set" && exit 1; \
    fi && \
    CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -ldflags="-w -s" -o flow2ftp ./cmd/flow2ftp && \
    chmod +x flow2ftp

# ================================
# 2. 构建阶段：编译 libfixbuf / YAF / super_mediator
# ================================

FROM debian:bookworm-slim AS builder

# ===== 多平台参数 =====
ARG TARGETPLATFORM
ARG BUILDPLATFORM

ARG APT_PROXY
ENV DEBIAN_FRONTEND=noninteractive
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig

ARG FIXBUF_VERSION=3.0.0.alpha2
ARG YAF_VERSION=3.0.0.alpha4
ARG SM_VERSION=2.0.0.alpha3

# 安装编译依赖
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
# 3. 运行阶段
# ================================

FROM debian:bookworm-slim AS runtime
ENV DEBIAN_FRONTEND=noninteractive

RUN set -eux; \
    rm -f /etc/apt/sources.list.d/debian.sources || true; \
    echo "deb http://mirrors.aliyun.com/debian bookworm main contrib non-free non-free-firmware" > /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian bookworm-updates main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
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

# 复制编译好的库和可执行文件
COPY --from=builder /usr/local/ /usr/local/

# flow2ftp 二进制（从 go-builder 阶段复制，需要在构建期自检之前）
COPY --from=go-builder /build/flow2ftp /usr/local/bin/flow2ftp
RUN chmod +x /usr/local/bin/flow2ftp

RUN echo "/usr/local/lib"  >  /etc/ld.so.conf.d/usr-local.conf; \
    echo "/usr/local/lib64" >> /etc/ld.so.conf.d/usr-local.conf; \
    ldconfig

ENV LD_LIBRARY_PATH=/usr/local/lib:/usr/local/lib64

# ===== 构建期自检：验证所有命令是否可用 =====
RUN set -eux; \
    echo "=== 构建期自检：验证可执行文件 ==="; \
    /usr/local/bin/yaf --version || (echo "ERROR: yaf not found or failed" && exit 1); \
    /usr/local/bin/super_mediator --version 2>&1 || echo "WARN: super_mediator --version not supported (continuing...)"; \
    /usr/local/bin/flow2ftp --help 2>&1 || echo "WARN: flow2ftp --help not supported (continuing...)"; \
    command -v /usr/local/bin/yaf >/dev/null || (echo "ERROR: yaf command not found" && exit 1); \
    command -v /usr/local/bin/super_mediator >/dev/null || (echo "ERROR: super_mediator command not found" && exit 1); \
    command -v /usr/local/bin/flow2ftp >/dev/null || (echo "ERROR: flow2ftp command not found" && exit 1); \
    ls -lh /usr/local/bin/yaf /usr/local/bin/super_mediator /usr/local/bin/flow2ftp; \
    echo "=== 构建期自检通过 ==="

# ===== 创建非 root 用户（一定要在任何 chown 之前）=====
RUN set -eux; \
    groupadd -r yaf && \
    useradd -r -g yaf -d /var/lib/yaf -s /usr/sbin/nologin yaf; \
    mkdir -p /var/lib/yaf

# 基础目录：/data、/etc/yaf、supervisor、/opt/yaf
RUN set -eux; \
    mkdir -p /data /etc/yaf /var/log/supervisor /var/run/supervisor /opt/yaf; \
    chown -R yaf:yaf /data /var/log/supervisor /var/run/supervisor /opt/yaf; \
    chown -R root:root /etc/yaf; \
    chmod 755 /var/run/supervisor

# ====== 拷贝默认配置文件 ======
COPY yaf.init /opt/yaf/yaf.init
RUN ln -sf /opt/yaf/yaf.init /etc/yaf/yaf.init

# 再确保权限（/opt/yaf 给 yaf，用来挂载/覆盖也方便）
RUN set -eux; \
    chown -R yaf:yaf /opt/yaf; \
    chown -R root:root /etc/yaf



# ================================
# 启动脚本
# ================================

RUN cat >/usr/local/bin/start_yaf.sh <<'EOF'
#!/usr/bin/env bash
set -eu

YAF_CONFIG_FILE="${YAF_CONFIG_FILE:-/etc/yaf/yaf.init}"

echo "[start_yaf] 使用 YAF 配置文件: ${YAF_CONFIG_FILE}"
if [ ! -f "${YAF_CONFIG_FILE}" ]; then
  echo "[start_yaf] ERROR: 未找到 ${YAF_CONFIG_FILE}" >&2
  exit 1
fi

exec /usr/local/bin/yaf --config "${YAF_CONFIG_FILE}" "$@"
EOF

RUN cat >/usr/local/bin/start_pipeline.sh <<'EOF'
#!/usr/bin/env bash
set -eu

YAF_CONFIG_FILE="${YAF_CONFIG_FILE:-/etc/yaf/yaf.init}"
SM_LISTEN_PORT="${SM_LISTEN_PORT:-18000}"
SM_FIELDS="${SM_FIELDS:-flowStartMilliseconds,flowEndMilliseconds,sourceIPv4Address,destinationIPv4Address,sourceTransportPort,destinationTransportPort,protocolIdentifier,silkAppLabel,octetTotalCount,packetTotalCount,initialTCPFlags,ipClassOfService,ingressInterface,egressInterface}"

echo "[start_pipeline] super_mediator 监听端口: ${SM_LISTEN_PORT}"
echo "[start_pipeline] TEXT 输出字段: ${SM_FIELDS}"

exec /bin/bash -c "
  /usr/local/bin/super_mediator \
    --ipfix-input=tcp \
    --ipfix-port=\"${SM_LISTEN_PORT}\" \
    --output-mode=TEXT \
    --print-headers \
    --out=- \
    --fields=\"${SM_FIELDS}\" \
    localhost \
  | /usr/local/bin/flow2ftp -config \"${YAF_CONFIG_FILE}\" -data-dir /data
"
EOF

RUN sed -i 's/\r$//' /usr/local/bin/start_yaf.sh /usr/local/bin/start_pipeline.sh
RUN chmod +x /usr/local/bin/start_yaf.sh /usr/local/bin/start_pipeline.sh



# ================================
# supervisor 配置
# ================================

RUN cat >/etc/supervisor/supervisord.conf <<'EOF'
[unix_http_server]
file=/var/run/supervisor/supervisor.sock
chmod=0700

[supervisord]
nodaemon=true
logfile=/dev/stdout
logfile_maxbytes=0
logfile_backups=0
pidfile=/var/run/supervisor/supervisord.pid
user=root

[program:yaf]
command=/bin/bash /usr/local/bin/start_yaf.sh
user=root
autorestart=true
startsecs=3
environment=PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",LD_LIBRARY_PATH="/usr/local/lib:/usr/local/lib64"
stdout_logfile=/var/log/supervisor/yaf.log
stdout_logfile_maxbytes=20MB
stdout_logfile_backups=5
stderr_logfile=/dev/stdout
stderr_logfile_maxbytes=0
stderr_logfile_backups=0

[program:pipeline]
command=/bin/bash /usr/local/bin/start_pipeline.sh
user=root
autorestart=true
startsecs=3
environment=PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",LD_LIBRARY_PATH="/usr/local/lib:/usr/local/lib64"
stdout_logfile=/var/log/supervisor/pipeline.log
stdout_logfile_maxbytes=20MB
stdout_logfile_backups=5
stderr_logfile=/dev/stdout
stderr_logfile_maxbytes=0
stderr_logfile_backups=0
EOF



# ================================
# 最终运行用户 + 目录
# ================================

# 注意：supervisord 需要以 root 运行才能管理其他进程
# 但我们在配置中指定了每个 program 以 yaf 用户运行
WORKDIR /opt/yaf

ENTRYPOINT ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]
CMD []
