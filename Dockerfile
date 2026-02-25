# 第一阶段：编译 (Builder)
FROM golang:1.25-alpine AS builder

# 设置工作目录
WORKDIR /app

# 先拷贝依赖文件，这样如果代码变了但依赖没变，可以跳过下载，速度极快
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源代码并编译
COPY . .
# 编译成名为 sst-proxy 的静态二进制文件
RUN CGO_ENABLED=0 GOOS=linux go build -o sst-proxy .

# 第二阶段：运行 (Runner)
FROM alpine:latest

# 💡 关键点：必须安装证书，否则无法建立跟 Google 的 HTTPS 连接
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# 从 builder 阶段只把编译好的“果实”拿过来
COPY --from=builder /app/sst-proxy .

# 暴露端口
EXPOSE 8080

# 启动命令
CMD ["./sst-proxy"]