# 使用官方Go镜像作为构建环境
FROM golang:1.24-alpine AS builder
ENV GO111MODULE=on 
ENV CGO_ENABLED=0 
ENV GOOS=linux 
ENV GOPROXY=https://goproxy.cn,direct 
ENV GOARCH=amd64
# 设置工作目录
WORKDIR /app

# 设置阿里云镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 复制go mod和sum文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o code-review cmd/server/main.go

# 使用alpine作为运行环境
FROM alpine:latest

# 安装ca-certificates以支持HTTPS请求
RUN apk --no-cache add ca-certificates

# 创建非root用户
RUN adduser -D -s /bin/sh appuser

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/code-review .

# 复制配置文件示例
# COPY --from=builder /app/.env.example .env

# 创建日志目录并设置权限
RUN mkdir -p log && chown -R appuser:appuser log

# 切换到非root用户
USER appuser

# 暴露端口
EXPOSE 8008

# 运行应用
CMD ["./code-review"]