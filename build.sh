#!/bin/bash

# 确保脚本出错时终止
set -e

# 定义编译目标平台
PLATFORMS=("darwin/amd64" "darwin/arm64" "linux/amd64" "linux/arm64")

# 获取当前目录名，用作输出文件名的一部分
DIR_NAME=$(basename "$(pwd)")

for platform in "${PLATFORMS[@]}"; do
    # 分割字符串成数组
    IFS="/" read -r -a os_arch <<< "$platform"

    # 设置目标操作系统和体系结构
    GOOS=${os_arch[0]}
    GOARCH=${os_arch[1]}
    OUTPUT_NAME="${DIR_NAME}-${GOOS}-${GOARCH}"

    if [ "$GOOS" = "windows" ]; then
        OUTPUT_NAME+='.exe'
    fi

    echo "Building for $GOOS/$GOARCH..."
    env CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -o "build/$OUTPUT_NAME"
done

echo "Build completed."
