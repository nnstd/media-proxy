FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache \
    gcc \
    musl-dev \
    pkgconfig \
    make \
    nasm \
    yasm \
    git \
    wget \
    tar \
    xz \
    openssl-dev \
    libwebp-dev \
    mupdf-dev

WORKDIR /app

# Set FFmpeg version to match go-astiav requirements (n7.0)
ENV FFMPEG_VERSION=n7.0

# Build FFmpeg from source following go-astiav Makefile approach
RUN mkdir -p tmp && \
    cd tmp && \
    wget https://github.com/FFmpeg/FFmpeg/archive/refs/tags/${FFMPEG_VERSION}.tar.gz && \
    tar -xzf ${FFMPEG_VERSION}.tar.gz && \
    cd FFmpeg-${FFMPEG_VERSION} && \
    ./configure \
        --prefix=/app/tmp/${FFMPEG_VERSION} \
        --enable-shared \
        --disable-static \
        --disable-autodetect \
        --disable-programs \
        --disable-doc \
        --disable-postproc \
        --disable-pixelutils \
        --disable-hwaccels \
        --disable-ffprobe \
        --disable-ffplay \
        --enable-openssl \
        --enable-protocol=file,http,hls && \
    make -j$(nproc) && \
    make install

# Set environment variables for Go build to match FFmpeg version
ENV CGO_LDFLAGS="-L/app/tmp/n7.0/lib/"
ENV CGO_CFLAGS="-I/app/tmp/n7.0/include/"
ENV PKG_CONFIG_PATH="/app/tmp/n7.0/lib/pkgconfig"

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -tags=musl -ldflags "-s -w" -o server .

FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    musl \
    openssl \
    libwebp \
    mupdf

WORKDIR /app

# Copy FFmpeg libraries from builder
COPY --from=builder /app/tmp/n7.0/lib/*.so* /usr/local/lib/
RUN ldconfig /usr/local/lib || true

COPY --from=builder /app/server .
EXPOSE 3000

CMD ["sh", "-c", "/app/server"]